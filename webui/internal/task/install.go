package task

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"os"

	"scrcpy-lan/webui/internal/device"
)

// InstallAPK 并行把 apkPath 装到每台设备：1) 推 apk 到
// /data/local/tmp/scrcpy-push-<随机>.apk；2) shell "pm install -r -t <path>"；
// 3) shell "rm <path>"。任一步失败记 Error。无 admin 地址的设备直接记
// Error（"admin address unavailable"），不 panic。
func InstallAPK(ctx context.Context, mgr *device.Manager, ids []string, apkPath string) []Result {
	if mgr == nil {
		return []Result{}
	}
	exec := execFor(mgr)
	return runBatch(ctx, ids, func(ctx context.Context, id string) Result {
		remote := "/data/local/tmp/scrcpy-push-" + randHex(8) + ".apk"
		if err := pushLocal(ctx, mgr, id, apkPath, remote); err != nil {
			return Result{ID: id, Ok: false, Error: err.Error(), Remote: remote}
		}
		if ctx.Err() != nil {
			return Result{ID: id, Ok: false, Error: ctx.Err().Error(), Remote: remote}
		}
		res, err := exec(id, "pm install -r -t "+remote)
		if err != nil {
			return Result{ID: id, Ok: false, Error: err.Error(), Remote: remote}
		}
		if res.ExitCode != 0 {
			return Result{
				ID:       id,
				Ok:       false,
				Error:    "pm install failed",
				ExitCode: res.ExitCode,
				Stdout:   res.Stdout,
				Stderr:   res.Stderr,
				Remote:   remote,
			}
		}
		// 清理临时文件：失败不视为安装失败，但保留 error 以助排查。
		if _, rmErr := exec(id, "rm "+remote); rmErr != nil {
			return Result{ID: id, Ok: true, Error: rmErr.Error(), Remote: remote}
		}
		return Result{ID: id, Ok: true, Remote: remote}
	})
}

// PushFile 并行把 localPath 推送到每台设备的 remotePath（如 /sdcard/Download/x）。
// 无 admin 地址的设备直接记 Error，不 panic。
func PushFile(ctx context.Context, mgr *device.Manager, ids []string, localPath, remotePath string) []Result {
	if mgr == nil {
		return []Result{}
	}
	return runBatch(ctx, ids, func(ctx context.Context, id string) Result {
		if err := pushLocal(ctx, mgr, id, localPath, remotePath); err != nil {
			return Result{ID: id, Ok: false, Error: err.Error(), Remote: remotePath}
		}
		return Result{ID: id, Ok: true, Remote: remotePath}
	})
}

// pushLocal 打开 localPath 并推送到 mgr 对应设备的 remotePath。它先尝试把
// mgr 断言为可选接口 adminDialer{ AdminAddr }；断言失败说明集成者尚未在
// device.Manager 上实现该方法，直接返回 errAdminAddrUnavailable。
func pushLocal(ctx context.Context, mgr *device.Manager, id, localPath, remotePath string) error {
	f, err := os.Open(localPath)
	if err != nil {
		return err
	}
	defer f.Close()

	st, err := f.Stat()
	if err != nil {
		return err
	}

	addr, err := resolveAdminAddr(mgr, id)
	if err != nil {
		return err
	}

	return PushTo(ctx, addr, remotePath, f, st.Size())
}

// resolveAdminAddr 从 mgr 解析设备 admin 地址。mgr 需实现可选接口
// adminDialer（集成者将在 device.Manager 上补 AdminAddr）；未实现返回
// errAdminAddrUnavailable。接受 any 便于测试注入非 Manager 的桩。
func resolveAdminAddr(mgr any, id string) (string, error) {
	d, ok := mgr.(adminDialer)
	if !ok {
		return "", errAdminAddrUnavailable
	}
	return d.AdminAddr(id)
}

// randHex 返回 2n 字符的小写十六进制随机串（n 字节熵）。
func randHex(n int) string {
	b := make([]byte, n)
	if _, err := rand.Read(b); err != nil {
		// crypto/rand 极少失败；失败时退回固定值保证可用。
		b = make([]byte, n)
	}
	return hex.EncodeToString(b)
}
