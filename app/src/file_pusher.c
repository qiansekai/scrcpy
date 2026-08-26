#include "file_pusher.h"

#include <assert.h>
#include <stdlib.h>
#include <string.h>

#include "adb/adb.h"
#include "control_msg.h"
#include "controller.h"
#include "file_pusher_lan.h"
#include "util/log.h"
#include "util/strbuf.h"

#define DEFAULT_PUSH_TARGET "/sdcard/Download/"

static void
sc_file_pusher_request_destroy(struct sc_file_pusher_request *req) {
    free(req->file);
}

bool
sc_file_pusher_init(struct sc_file_pusher *fp, struct sc_controller *controller,
                    const char *serial, uint32_t tunnel_host,
                    uint16_t tunnel_port, const char *push_target) {
    assert(controller);
    assert(serial || tunnel_host);

    sc_vecdeque_init(&fp->queue);

    bool ok = sc_mutex_init(&fp->mutex);
    if (!ok) {
        return false;
    }

    ok = sc_cond_init(&fp->event_cond);
    if (!ok) {
        sc_mutex_destroy(&fp->mutex);
        return false;
    }

    ok = sc_intr_init(&fp->intr);
    if (!ok) {
        sc_cond_destroy(&fp->event_cond);
        sc_mutex_destroy(&fp->mutex);
        return false;
    }

    if (serial) {
        fp->serial = strdup(serial);
        if (!fp->serial) {
            LOG_OOM();
            sc_intr_destroy(&fp->intr);
            sc_cond_destroy(&fp->event_cond);
            sc_mutex_destroy(&fp->mutex);
            return false;
        }
    } else {
        fp->serial = NULL;
    }
    fp->tunnel_host = tunnel_host;
    fp->tunnel_port = tunnel_port;

    // lazy initialization
    fp->initialized = false;

    fp->stopped = false;

    fp->push_target = push_target ? push_target : DEFAULT_PUSH_TARGET;
    fp->controller = controller;

    return true;
}

void
sc_file_pusher_destroy(struct sc_file_pusher *fp) {
    sc_cond_destroy(&fp->event_cond);
    sc_mutex_destroy(&fp->mutex);
    sc_intr_destroy(&fp->intr);
    free(fp->serial);

    while (!sc_vecdeque_is_empty(&fp->queue)) {
        struct sc_file_pusher_request *req = sc_vecdeque_popref(&fp->queue);
        assert(req);
        sc_file_pusher_request_destroy(req);
    }
    sc_vecdeque_destroy(&fp->queue);
}

bool
sc_file_pusher_request(struct sc_file_pusher *fp,
                       enum sc_file_pusher_action action, char *file) {
    // start file_pusher if it's used for the first time
    if (!fp->initialized) {
        if (!sc_file_pusher_start(fp)) {
            return false;
        }
        fp->initialized = true;
    }

    LOGI("Request to %s %s", action == SC_FILE_PUSHER_ACTION_INSTALL_APK
                                 ? "install" : "push",
                             file);
    struct sc_file_pusher_request req = {
        .action = action,
        .file = file,
    };

    sc_mutex_lock(&fp->mutex);
    bool was_empty = sc_vecdeque_is_empty(&fp->queue);
    bool res = sc_vecdeque_push(&fp->queue, req);
    if (!res) {
        LOG_OOM();
        sc_mutex_unlock(&fp->mutex);
        return false;
    }

    if (was_empty) {
        sc_cond_signal(&fp->event_cond);
    }
    sc_mutex_unlock(&fp->mutex);

    return true;
}

static bool
request_scan_file(struct sc_file_pusher *fp) {
    struct sc_control_msg msg;
    msg.type = SC_CONTROL_MSG_TYPE_SCAN_FILE;
    msg.scan_file.path = strdup(fp->push_target);
    if (!msg.scan_file.path) {
        LOG_OOM();
        return false;
    }

    if (!sc_controller_push_msg(fp->controller, &msg)) {
        LOGW("Could not request 'scan file'");
        return false;
    }

    return true;
}

// 取 Windows/Unix 路径的 basename，并把引号等特殊字符替换为 '_'，
// 避免注入 shell 命令或破坏远程路径。
static char *
safe_basename(const char *path) {
    const char *base = path;
    for (const char *p = path; *p; ++p) {
        if (*p == '/' || *p == '\\') {
            base = p + 1;
        }
    }
    char *name = strdup(base);
    if (name) {
        for (char *p = name; *p; ++p) {
            if (*p == '"' || *p == '\'' || *p == '`' || *p == '$'
                    || *p == ';' || *p == '&' || *p == '|') {
                *p = '_';
            }
        }
    }
    return name;
}

// LAN 模式：推文件到设备 admin 通道后执行 pm install -r。
static void
lan_install_apk(struct sc_file_pusher *fp, const char *file) {
    char *name = safe_basename(file);
    if (!name) {
        LOG_OOM();
        return;
    }

    struct sc_strbuf remote;
    bool ok = sc_strbuf_init(&remote, 128);
    if (!ok) {
        free(name);
        return;
    }
    sc_strbuf_append_staticstr(&remote, "/data/local/tmp/scrcpy-push-");
    sc_strbuf_append_str(&remote, name);
    sc_strbuf_shrink(&remote);

    LOGI("LAN push: sending %s...", file);
    ok = sc_file_pusher_lan_push(fp->tunnel_host, fp->tunnel_port, file,
                                 remote.s);
    if (ok) {
        LOGI("LAN push: done, installing via pm install -r...");
        struct sc_strbuf cmd;
        if (sc_strbuf_init(&cmd, 128)) {
            sc_strbuf_append_staticstr(&cmd, "pm install -r -t \"");
            sc_strbuf_append_str(&cmd, remote.s);
            sc_strbuf_append_staticstr(&cmd, "\"");
            sc_strbuf_shrink(&cmd);

            char *out = NULL;
            ok = sc_file_pusher_lan_shell(fp->tunnel_host, fp->tunnel_port,
                                          cmd.s, &out);
            if (out && *out) {
                LOGI("LAN install output:\n%s", out);
            }
            free(out);
            if (ok) {
                LOGI("LAN install: %s successfully installed", file);
            } else {
                LOGE("LAN install: pm install failed for %s", file);
            }
            free(cmd.s);
        }

        // 清理设备端临时 apk
        struct sc_strbuf rm;
        if (sc_strbuf_init(&rm, 128)) {
            sc_strbuf_append_staticstr(&rm, "rm \"");
            sc_strbuf_append_str(&rm, remote.s);
            sc_strbuf_append_staticstr(&rm, "\"");
            sc_strbuf_shrink(&rm);
            sc_file_pusher_lan_shell(fp->tunnel_host, fp->tunnel_port,
                                     rm.s, NULL);
            free(rm.s);
        }
    } else {
        LOGE("LAN push: failed to send %s", file);
    }

    free(remote.s);
    free(name);
}

// LAN 模式：推文件到 push_target 目录并触发媒体扫描。
static void
lan_push_file(struct sc_file_pusher *fp, const char *file) {
    char *name = safe_basename(file);
    if (!name) {
        LOG_OOM();
        return;
    }

    struct sc_strbuf remote;
    bool ok = sc_strbuf_init(&remote, 128);
    if (!ok) {
        free(name);
        return;
    }
    sc_strbuf_append_str(&remote, fp->push_target);
    // push_target 可能不带结尾斜杠（--push-target=/sdcard/Download），补上
    if (remote.len && remote.s[remote.len - 1] != '/') {
        sc_strbuf_append_char(&remote, '/');
    }
    sc_strbuf_append_str(&remote, name);
    sc_strbuf_shrink(&remote);

    LOGI("LAN push: sending %s...", file);
    ok = sc_file_pusher_lan_push(fp->tunnel_host, fp->tunnel_port, file,
                                 remote.s);
    if (ok) {
        LOGI("LAN push: %s successfully pushed to %s", file, remote.s);
        request_scan_file(fp); // any error already logged
    } else {
        LOGE("LAN push: failed to push %s to %s", file, remote.s);
    }

    free(remote.s);
    free(name);
}

static int
run_file_pusher(void *data) {
    struct sc_file_pusher *fp = data;
    struct sc_intr *intr = &fp->intr;

    const char *serial = fp->serial;
    const char *push_target = fp->push_target;
    assert(push_target);

    for (;;) {
        sc_mutex_lock(&fp->mutex);
        while (!fp->stopped && sc_vecdeque_is_empty(&fp->queue)) {
            sc_cond_wait(&fp->event_cond, &fp->mutex);
        }
        if (fp->stopped) {
            // stop immediately, do not process further events
            sc_mutex_unlock(&fp->mutex);
            break;
        }

        assert(!sc_vecdeque_is_empty(&fp->queue));
        struct sc_file_pusher_request req = sc_vecdeque_pop(&fp->queue);
        sc_mutex_unlock(&fp->mutex);

        if (!serial) {
            // 无 adb（LAN）模式：走设备 admin 通道（27184）自推送 + pm install
            if (req.action == SC_FILE_PUSHER_ACTION_INSTALL_APK) {
                lan_install_apk(fp, req.file);
            } else {
                lan_push_file(fp, req.file);
            }
        } else if (req.action == SC_FILE_PUSHER_ACTION_INSTALL_APK) {
            LOGI("Installing %s...", req.file);
            bool ok = sc_adb_install(intr, serial, req.file, 0);
            if (ok) {
                LOGI("%s successfully installed", req.file);
            } else {
                LOGE("Failed to install %s", req.file);
            }
        } else {
            LOGI("Pushing %s...", req.file);
            bool ok = sc_adb_push(intr, serial, req.file, push_target, 0);
            if (ok) {
                LOGI("%s successfully pushed to %s", req.file, push_target);
                request_scan_file(fp); // any error already logged
            } else {
                LOGE("Failed to push %s to %s", req.file, push_target);
            }
        }

        sc_file_pusher_request_destroy(&req);
    }
    return 0;
}

bool
sc_file_pusher_start(struct sc_file_pusher *fp) {
    LOGD("Starting file_pusher thread");

    bool ok = sc_thread_create(&fp->thread, run_file_pusher, "scrcpy-file", fp);
    if (!ok) {
        LOGE("Could not start file_pusher thread");
        return false;
    }

    return true;
}

void
sc_file_pusher_stop(struct sc_file_pusher *fp) {
    if (fp->initialized) {
        sc_mutex_lock(&fp->mutex);
        fp->stopped = true;
        sc_cond_signal(&fp->event_cond);
        sc_intr_interrupt(&fp->intr);
        sc_mutex_unlock(&fp->mutex);
    }
}

void
sc_file_pusher_join(struct sc_file_pusher *fp) {
    if (fp->initialized) {
        sc_thread_join(&fp->thread, NULL);
    }
}
