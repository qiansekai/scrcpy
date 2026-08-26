#ifndef SC_FILE_PUSHER_LAN_H
#define SC_FILE_PUSHER_LAN_H

#include "common.h"

#include <stdbool.h>
#include <stdint.h>

/**
 * LAN 文件推送（无 adb）：
 * 走设备端 AdminServer（默认 27184）的 TYPE_PUSH 分块协议把本地文件写到
 * 设备端 remote 路径（如 /data/local/tmp/scrcpy-push-xxx.apk），每块带 ack，
 * 不会像控制通道队列那样丢块。
 */
bool
sc_file_pusher_lan_push(uint32_t host, uint16_t port, const char *local,
                        const char *remote);

/**
 * 在设备上执行一条 shell 命令（admin TYPE_SHELL）。out 非空时接收命令的
 * stdout/stderr（调用方负责 free）。
 */
bool
sc_file_pusher_lan_shell(uint32_t host, uint16_t port, const char *cmd,
                         char **out);

#endif
