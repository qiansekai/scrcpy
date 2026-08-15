package httpapi

import (
	"encoding/json"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"scrcpy-lan/webui/internal/control"
	"scrcpy-lan/webui/internal/device"
	"scrcpy-lan/webui/internal/store"
)

// noopBroadcaster 实现 device.Broadcaster 空方法，测试内不关心发布结果。
type noopBroadcaster struct{}

func (noopBroadcaster) PublishSession(string, device.SessionInfo)           {}
func (noopBroadcaster) PublishFrame(string, *device.VideoFrame)             {}
func (noopBroadcaster) PublishDeviceMessage(string, *control.DeviceMessage) {}

func TestDeviceLifecycle(t *testing.T) {
	cfg := &store.Config{}
	mgr := device.NewManager(&noopBroadcaster{})
	h := New(cfg, mgr, "")

	// 空列表
	req := httptest.NewRequest("GET", "/api/devices", nil)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != 200 || !strings.Contains(rec.Body.String(), `[]`) {
		t.Fatalf("list = %d %s", rec.Code, rec.Body.String())
	}

	// 添加不可达 IP → 502
	body := strings.NewReader(`{"ip":"127.0.0.1:1"}`)
	req = httptest.NewRequest("POST", "/api/devices", body)
	rec = httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusBadGateway {
		t.Fatalf("add unreachable = %d, want 502", rec.Code)
	}

	_ = json.Marshal
}

// startFakeDevice 监听本地端口，accept 后立即写出 scrcpy 视频握手
// (dummy + 64B 设备名 + codec) 再关闭。probe 与 StreamSession 的连接都
// 能快速得到确定性结果，不会挂起。
func startFakeDevice(t *testing.T) net.Listener {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	t.Cleanup(func() { ln.Close() })
	go func() {
		for {
			c, err := ln.Accept()
			if err != nil {
				return
			}
			go func(c net.Conn) {
				defer c.Close()
				c.Write([]byte{0x00})
				name := make([]byte, device.DeviceNameLen)
				copy(name, "FakePhone")
				c.Write(name)
				c.Write(device.CodecH264[:])
			}(c)
		}
	}()
	return ln
}

func TestAddReachablePersistsAndLists(t *testing.T) {
	ln := startFakeDevice(t)
	configPath := filepath.Join(t.TempDir(), "devices.json")
	cfg := &store.Config{}
	mgr := device.NewManager(&noopBroadcaster{})
	h := New(cfg, mgr, configPath)

	ip := ln.Addr().String()
	req := httptest.NewRequest("POST", "/api/devices", strings.NewReader(`{"ip":"`+ip+`"}`))
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusCreated {
		t.Fatalf("add reachable = %d, body=%s", rec.Code, rec.Body.String())
	}
	var added struct {
		ID   string `json:"id"`
		Name string `json:"name"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &added); err != nil {
		t.Fatalf("bad add response: %v", err)
	}
	if added.Name != "FakePhone" {
		t.Fatalf("name = %q", added.Name)
	}
	id := added.ID
	if id == "" || id != sanitizeID(ip) {
		t.Fatalf("id = %q, want sanitize(%s)=%q", id, ip, sanitizeID(ip))
	}
	if !mgr.Has(id) {
		t.Fatal("manager missing added device")
	}
	defer mgr.Remove(id)

	// 配置已落盘
	data, err := os.ReadFile(configPath)
	if err != nil {
		t.Fatalf("config not persisted: %v", err)
	}
	var saved store.Config
	if err := json.Unmarshal(data, &saved); err != nil {
		t.Fatalf("bad saved config: %v", err)
	}
	if len(saved.Devices) != 1 || saved.Devices[0].ID != id {
		t.Fatalf("saved devices = %+v", saved.Devices)
	}

	// 列表反映 online 状态
	req = httptest.NewRequest("GET", "/api/devices", nil)
	rec = httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	var list []deviceDTO
	if err := json.Unmarshal(rec.Body.Bytes(), &list); err != nil {
		t.Fatalf("bad list: %v", err)
	}
	if len(list) != 1 || !list[0].Online {
		t.Fatalf("list = %+v", list)
	}
}

func TestDeleteRemovesFromManagerAndConfig(t *testing.T) {
	ln := startFakeDevice(t)
	configPath := filepath.Join(t.TempDir(), "devices.json")
	id := "dev-1"
	cfg := &store.Config{Devices: []store.DeviceConfig{{ID: id, IP: "127.0.0.1"}}}
	mgr := device.NewManager(&noopBroadcaster{})
	mgr.Add(device.SessionConfig{ID: id, IP: "127.0.0.1", Addr: ln.Addr().String()})
	h := New(cfg, mgr, configPath)

	req := httptest.NewRequest("DELETE", "/api/devices/"+id, nil)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusNoContent {
		t.Fatalf("delete = %d", rec.Code)
	}
	if mgr.Has(id) {
		t.Fatal("manager still has device")
	}
	if len(cfg.Devices) != 0 {
		t.Fatalf("cfg devices = %+v", cfg.Devices)
	}
	data, err := os.ReadFile(configPath)
	if err != nil {
		t.Fatalf("config not persisted: %v", err)
	}
	var saved store.Config
	if err := json.Unmarshal(data, &saved); err != nil {
		t.Fatalf("bad saved config: %v", err)
	}
	if len(saved.Devices) != 0 {
		t.Fatalf("saved devices = %+v", saved.Devices)
	}
}

func TestStaticFallbackWhenNoDist(t *testing.T) {
	cfg := &store.Config{}
	mgr := device.NewManager(&noopBroadcaster{})
	h := New(cfg, mgr, "")

	req := httptest.NewRequest("GET", "/", nil)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != 200 || !strings.Contains(rec.Body.String(), "web/dist") {
		t.Fatalf("fallback = %d %s", rec.Code, rec.Body.String())
	}
}
