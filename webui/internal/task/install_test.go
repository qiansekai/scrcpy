package task

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"scrcpy-lan/webui/internal/device"
)

// fakeAdminManager 实现 device 的会话子集 + adminDialer，用于测 push/admin 地址路径。
type fakeAdminManager struct {
	addrs map[string]string
	errs  map[string]error
}

func (f *fakeAdminManager) AdminAddr(id string) (string, error) {
	if e := f.errs[id]; e != nil {
		return "", e
	}
	return f.addrs[id], nil
}

// writeTempFile 写一个临时文件并返回路径，测试结束删除。
func writeTempFile(t *testing.T, name, content string) string {
	t.Helper()
	p := filepath.Join(t.TempDir(), name)
	if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
		t.Fatalf("write temp: %v", err)
	}
	return p
}

// TestPushFile_EmptyAndNil 验证空 ids 与 nil mgr 安全。
func TestPushFile_EmptyAndNil(t *testing.T) {
	if got := PushFile(context.Background(), nil, nil, "x", "/sdcard/y"); got == nil || len(got) != 0 {
		t.Fatalf("empty = %#v", got)
	}
	if got := PushFile(context.Background(), nil, []string{}, "x", "/sdcard/y"); got == nil || len(got) != 0 {
		t.Fatalf("empty ids = %#v", got)
	}
}

// TestInstallAPK_EmptyAndNil 验证空 ids 与 nil mgr 安全。
func TestInstallAPK_EmptyAndNil(t *testing.T) {
	if got := InstallAPK(context.Background(), nil, nil, "x.apk"); got == nil || len(got) != 0 {
		t.Fatalf("empty = %#v", got)
	}
	if got := InstallAPK(context.Background(), nil, []string{}, "x.apk"); got == nil || len(got) != 0 {
		t.Fatalf("empty ids = %#v", got)
	}
}

// TestPushFile_LocalMissing 验证本地文件不存在返回错误（不依赖 admin 地址）。
func TestPushFile_LocalMissing(t *testing.T) {
	mgr := &device.Manager{}
	got := PushFile(context.Background(), mgr, []string{"a"}, "/no/such/file.apk", "/sdcard/x")
	if len(got) != 1 || got[0].Ok || got[0].Error == "" {
		t.Fatalf("got = %+v, want local-open error", got)
	}
}

// TestInstallAPK_NoSession 验证 manager 已实现 AdminAddr 但设备无会话时，
// push 阶段因 AdminAddr 返回 error（io.ErrClosedPipe）而失败并记 Error。
func TestInstallAPK_NoSession(t *testing.T) {
	old := execCommandFunc
	defer func() { execCommandFunc = old }()

	execCommandFunc = func(id, cmd string) (device.ExecResult, error) {
		return device.ExecResult{ExitCode: 0}, nil
	}
	apk := writeTempFile(t, "a.apk", "fakeapk")

	// 当前 device.Manager 已带 AdminAddr，但无会话 → AdminAddr 返回 error。
	mgr := &device.Manager{}
	got := InstallAPK(context.Background(), mgr, []string{"dev"}, apk)
	if len(got) != 1 {
		t.Fatalf("len = %d, want 1", len(got))
	}
	if got[0].Ok || got[0].Error == "" {
		t.Fatalf("got = %+v, want push error", got[0])
	}
	if got[0].Remote == "" || !strings.HasPrefix(got[0].Remote, "/data/local/tmp/scrcpy-push-") {
		t.Fatalf("remote = %q, want /data/local/tmp/scrcpy-push-*.apk", got[0].Remote)
	}
}

// TestPushFile_NoSession 验证 PushFile 在设备无会话时 push 失败记 Error。
func TestPushFile_NoSession(t *testing.T) {
	f := writeTempFile(t, "f.txt", "data")
	mgr := &device.Manager{}
	got := PushFile(context.Background(), mgr, []string{"dev"}, f, "/sdcard/f.txt")
	if len(got) != 1 || got[0].Ok || got[0].Error == "" {
		t.Fatalf("got = %+v, want push error", got[0])
	}
	if got[0].Remote != "/sdcard/f.txt" {
		t.Fatalf("remote = %q", got[0].Remote)
	}
}

// TestResolveAdminAddr_Unavailable 验证非 adminDialer 实现时返回 errAdminAddrUnavailable。
func TestResolveAdminAddr_Unavailable(t *testing.T) {
	// 用不实现 AdminAddr 的桩类型触发类型断言失败分支。
	notImpl := struct{}{}
	if _, err := resolveAdminAddr(notImpl, "dev"); err != errAdminAddrUnavailable {
		t.Fatalf("err = %v, want errAdminAddrUnavailable", err)
	}
}

// TestResolveAdminAddr_Error 验证 adminDialer.AdminAddr 返回错误时透传。
func TestResolveAdminAddr_Error(t *testing.T) {
	d := &fakeAdminManager{errs: map[string]error{"dev": errors.New("no addr")}}
	if addr, err := resolveAdminAddr(d, "dev"); err == nil || err.Error() != "no addr" || addr != "" {
		t.Fatalf("resolveAdminAddr = %q, %v; want error no addr", addr, err)
	}
}

// TestResolveAdminAddr_OK 验证 adminDialer 正常返回地址。
func TestResolveAdminAddr_OK(t *testing.T) {
	d := &fakeAdminManager{addrs: map[string]string{"dev": "1.2.3.4:27184"}}
	if got, err := resolveAdminAddr(d, "dev"); err != nil || got != "1.2.3.4:27184" {
		t.Fatalf("resolveAdminAddr = %q, %v", got, err)
	}
}
