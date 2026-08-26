#include "file_pusher_lan.h"

#include <stdint.h>
#include <stdio.h>
#include <stdlib.h>
#include <string.h>

#include "util/log.h"
#include "util/net.h"
#include "util/strbuf.h"

// admin 协议（与 server AdminServer.java / webui Go task 包一致）：
//   请求帧  [1B type][4B len][payload]
//   推送帧  type=0x11, payload=[4B pathLen][path UTF-8][data]，每块回 ack
//   shell 帧 type=0x10, payload=命令 UTF-8
//   响应帧  type=0x20 流式输出 / 0x21 结果(payload 前 4 字节 = exitCode)
#define ADMIN_TYPE_SHELL  0x10
#define ADMIN_TYPE_PUSH   0x11
#define ADMIN_TYPE_STREAM 0x20
#define ADMIN_TYPE_RESULT 0x21

#define ADMIN_DEFAULT_PORT 27184
#define PUSH_CHUNK_SIZE (256 * 1024)

static uint16_t
admin_port(uint16_t port) {
    return port ? port : ADMIN_DEFAULT_PORT;
}

static sc_socket
lan_connect(uint32_t host, uint16_t port) {
    sc_socket socket = net_socket();
    if (socket == SC_SOCKET_NONE) {
        return SC_SOCKET_NONE;
    }
    if (!net_connect(socket, host, admin_port(port))) {
        net_close(socket);
        return SC_SOCKET_NONE;
    }
    return socket;
}

static void
write_be32(uint8_t *buf, uint32_t v) {
    buf[0] = (uint8_t) (v >> 24);
    buf[1] = (uint8_t) (v >> 16);
    buf[2] = (uint8_t) (v >> 8);
    buf[3] = (uint8_t) v;
}

static uint32_t
read_be32(const uint8_t *buf) {
    return ((uint32_t) buf[0] << 24)
         | ((uint32_t) buf[1] << 16)
         | ((uint32_t) buf[2] << 8)
         | (uint32_t) buf[3];
}

// 读一个响应帧（type+len+payload 已分配，调用方释放 payload）。
static bool
lan_read_frame(sc_socket socket, uint8_t *type, uint8_t **payload,
               uint32_t *len) {
    uint8_t header[5];
    if (!net_recv_all(socket, header, sizeof(header))) {
        return false;
    }
    *type = header[0];
    *len = read_be32(&header[1]);
    if (*len > 4 * 1024 * 1024) {
        LOGE("LAN push: absurd frame length %u", *len);
        return false;
    }
    *payload = malloc(*len ? *len : 1);
    if (!*payload) {
        LOG_OOM();
        return false;
    }
    if (*len && !net_recv_all(socket, *payload, *len)) {
        free(*payload);
        return false;
    }
    return true;
}

// 读推送 ack：必须 type=0x21 且 payload 前 4 字节 exitCode==0。
static bool
lan_read_ack(sc_socket socket) {
    uint8_t type;
    uint8_t *payload;
    uint32_t len;
    if (!lan_read_frame(socket, &type, &payload, &len)) {
        return false;
    }
    bool ok = type == ADMIN_TYPE_RESULT && len >= 4
           && read_be32(payload) == 0;
    if (!ok) {
        LOGE("LAN push: bad ack (type=0x%02x len=%u)", type, len);
    }
    free(payload);
    return ok;
}

// 发送一帧推送数据块。
static bool
lan_send_chunk(sc_socket socket, const char *path, const uint8_t *data,
               size_t data_len) {
    size_t path_len = strlen(path);
    uint32_t frame_len = 4 + (uint32_t) path_len + (uint32_t) data_len;

    uint8_t *frame = malloc(5 + frame_len);
    if (!frame) {
        LOG_OOM();
        return false;
    }
    frame[0] = ADMIN_TYPE_PUSH;
    write_be32(&frame[1], frame_len);
    write_be32(&frame[5], (uint32_t) path_len);
    memcpy(&frame[9], path, path_len);
    if (data_len) {
        memcpy(&frame[9 + path_len], data, data_len);
    }

    bool ok = net_send_all(socket, frame, 5 + frame_len);
    free(frame);
    return ok;
}

bool
sc_file_pusher_lan_push(uint32_t host, uint16_t port, const char *local,
                        const char *remote) {
    FILE *f = fopen(local, "rb");
    if (!f) {
        LOGE("LAN push: cannot open %s", local);
        return false;
    }

    sc_socket socket = lan_connect(host, port);
    if (socket == SC_SOCKET_NONE) {
        LOGE("LAN push: cannot connect to device admin port");
        fclose(f);
        return false;
    }

    uint8_t *buf = malloc(PUSH_CHUNK_SIZE);
    if (!buf) {
        LOG_OOM();
        fclose(f);
        net_close(socket);
        return false;
    }

    bool ok = true;
    bool first = true;
    for (;;) {
        size_t r = fread(buf, 1, PUSH_CHUNK_SIZE, f);
        if (r > 0) {
            if (!lan_send_chunk(socket, remote, buf, r)
                    || !lan_read_ack(socket)) {
                ok = false;
                break;
            }
            first = false;
        }
        if (r < PUSH_CHUNK_SIZE) {
            if (ferror(f)) {
                ok = false;
            } else if (first) {
                // 空文件：补发一个空块完成"首块截断创建"语义
                ok = lan_send_chunk(socket, remote, NULL, 0)
                  && lan_read_ack(socket);
            }
            break;
        }
    }

    free(buf);
    fclose(f);
    net_close(socket);
    return ok;
}

// strbuf 要求追加"非空且 NUL 结尾"的字符串，而协议 payload 是裸字节，
// 这里包一层：len==0 跳过，否则复制到 NUL 结尾的临时缓冲再追加。
static bool
lan_append(struct sc_strbuf *buf, const uint8_t *data, uint32_t len) {
    if (len == 0) {
        return true;
    }
    char *tmp = malloc(len + 1);
    if (!tmp) {
        LOG_OOM();
        return false;
    }
    memcpy(tmp, data, len);
    tmp[len] = '\0';
    bool ok = sc_strbuf_append(buf, tmp, len);
    free(tmp);
    return ok;
}

bool
sc_file_pusher_lan_shell(uint32_t host, uint16_t port, const char *cmd,
                         char **out) {
    sc_socket socket = lan_connect(host, port);
    if (socket == SC_SOCKET_NONE) {
        LOGE("LAN shell: cannot connect to device admin port");
        return false;
    }

    size_t cmd_len = strlen(cmd);
    uint8_t *req = malloc(5 + cmd_len);
    if (!req) {
        LOG_OOM();
        net_close(socket);
        return false;
    }
    req[0] = ADMIN_TYPE_SHELL;
    write_be32(&req[1], (uint32_t) cmd_len);
    memcpy(&req[5], cmd, cmd_len);
    bool ok = net_send_all(socket, req, 5 + cmd_len);
    free(req);
    if (!ok) {
        net_close(socket);
        return false;
    }

    struct sc_strbuf buf;
    if (!sc_strbuf_init(&buf, 256)) {
        net_close(socket);
        return false;
    }

    for (;;) {
        uint8_t type;
        uint8_t *payload;
        uint32_t len;
        if (!lan_read_frame(socket, &type, &payload, &len)) {
            ok = false;
            break;
        }
        if (type == ADMIN_TYPE_STREAM) {
            if (!lan_append(&buf, payload, len)) {
                free(payload);
                ok = false;
                break;
            }
        } else if (type == ADMIN_TYPE_RESULT) {
            if (len >= 4) {
                // payload = exitCode(4) + stdoutLen(4) + stdout + stderrLen(4) + stderr
                uint32_t exit_code = read_be32(payload);
                LOGI("LAN shell exit=%u", exit_code);
                if (len >= 12) {
                    uint32_t so_len = read_be32(&payload[4]);
                    uint32_t se_off = 8 + so_len;
                    if (se_off + 4 <= len) {
                        uint32_t se_len = read_be32(&payload[se_off]);
                        if (se_off + 4 + se_len <= len) {
                            lan_append(&buf, &payload[8], so_len);
                            lan_append(&buf, &payload[se_off + 4], se_len);
                        }
                    }
                }
                ok = exit_code == 0;
            } else {
                ok = false;
            }
            free(payload);
            break;
        } else {
            free(payload);
            continue;
        }
        free(payload);
    }

    sc_strbuf_shrink(&buf);
    if (out) {
        *out = buf.s;
    } else {
        free(buf.s);
    }
    net_close(socket);
    return ok;
}
