// 临时验证 harness：调用 sc_file_pusher_lan_push/shell 对真实设备做推送+安装测试
#include "file_pusher_lan.h"
#include "util/net.h"

#include <stdio.h>
#include <stdlib.h>
#include <string.h>

int
main(int argc, char *argv[]) {
    net_init();
    if (argc < 3) {
        fprintf(stderr, "usage: %s <host-ip> <local-file>\n", argv[0]);
        return 1;
    }
    uint32_t host = 0;
    unsigned a, b, c, d;
    if (sscanf(argv[1], "%u.%u.%u.%u", &a, &b, &c, &d) != 4) {
        fprintf(stderr, "bad ip\n");
        return 1;
    }
    host = (a << 24) | (b << 16) | (c << 8) | d;

    const char *name = strrchr(argv[2], '/');
    if (!name) {
        name = strrchr(argv[2], '\\');
    }
    name = name ? name + 1 : argv[2];

    char remote[512];
    snprintf(remote, sizeof(remote), "/data/local/tmp/scrcpy-push-%s", name);

    printf("push %s -> %s\n", argv[2], remote);
    bool ok = sc_file_pusher_lan_push(host, 0, argv[2], remote);
    printf("push: %s\n", ok ? "OK" : "FAIL");
    if (!ok) {
        return 1;
    }

    char cmd[600];
    snprintf(cmd, sizeof(cmd), "pm install -r -t \"%s\"", remote);
    char *out = NULL;
    ok = sc_file_pusher_lan_shell(host, 0, cmd, &out);
    printf("install: %s\n%s\n", ok ? "OK" : "FAIL", out ? out : "");
    free(out);

    snprintf(cmd, sizeof(cmd), "rm \"%s\"", remote);
    sc_file_pusher_lan_shell(host, 0, cmd, NULL);
    return ok ? 0 : 1;
}
