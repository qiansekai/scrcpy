#ifndef SC_FILE_PUSHER_H
#define SC_FILE_PUSHER_H

#include "common.h"

#include <stdbool.h>

#include "util/intr.h"
#include "util/thread.h"
#include "util/vecdeque.h"

struct sc_controller;

enum sc_file_pusher_action {
    SC_FILE_PUSHER_ACTION_INSTALL_APK,
    SC_FILE_PUSHER_ACTION_PUSH_FILE,
};

struct sc_file_pusher_request {
    enum sc_file_pusher_action action;
    char *file;
};

struct sc_file_pusher_request_queue SC_VECDEQUE(struct sc_file_pusher_request);

struct sc_file_pusher {
    struct sc_controller *controller; // used to send SCAN_FILE requests
    char *serial;                     // NULL 表示无 adb（LAN 模式）
    uint32_t tunnel_host;             // LAN 模式：admin 通道主机
    uint16_t tunnel_port;             // LAN 模式：admin 通道端口（0=默认 27184）
    const char *push_target;
    sc_thread thread;
    sc_mutex mutex;
    sc_cond event_cond;
    bool stopped;
    bool initialized;
    struct sc_file_pusher_request_queue queue;

    struct sc_intr intr;
};

bool
sc_file_pusher_init(struct sc_file_pusher *fp, struct sc_controller *controller,
                    const char *serial, uint32_t tunnel_host,
                    uint16_t tunnel_port, const char *push_target);

void
sc_file_pusher_destroy(struct sc_file_pusher *fp);

bool
sc_file_pusher_start(struct sc_file_pusher *fp);

void
sc_file_pusher_stop(struct sc_file_pusher *fp);

void
sc_file_pusher_join(struct sc_file_pusher *fp);

// take ownership of file, and will free() it
bool
sc_file_pusher_request(struct sc_file_pusher *fp,
                       enum sc_file_pusher_action action, char *file);

#endif
