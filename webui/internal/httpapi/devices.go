package httpapi

import (
	"encoding/json"
	"io"
	"net"
	"net/http"
	"strings"
	"time"

	"scrcpy-lan/webui/internal/device"
	"scrcpy-lan/webui/internal/store"
)

type api struct {
	cfg        *store.Config
	mgr        *device.Manager
	configPath string
}

type deviceDTO struct {
	ID     string `json:"id"`
	IP     string `json:"ip"`
	Online bool   `json:"online"`
	Name   string `json:"name,omitempty"`
}

func (a *api) handleDevices(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		a.list(w)
	case http.MethodPost:
		a.add(w, r)
	default:
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	}
}

func (a *api) handleDevice(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodDelete {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	id := strings.TrimPrefix(r.URL.Path, "/api/devices/")
	a.remove(w, id)
}

func (a *api) list(w http.ResponseWriter) {
	out := []deviceDTO{}
	for _, d := range a.cfg.Devices {
		out = append(out, deviceDTO{ID: d.ID, IP: d.IP, Online: a.mgr.Has(d.ID)})
	}
	writeJSON(w, http.StatusOK, out)
}

func (a *api) add(w http.ResponseWriter, r *http.Request) {
	var req struct {
		IP string `json:"ip"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.IP == "" {
		http.Error(w, "bad request", http.StatusBadRequest)
		return
	}
	name, err := probeDevice(req.IP)
	if err != nil {
		http.Error(w, "device unreachable: "+err.Error(), http.StatusBadGateway)
		return
	}
	id := sanitizeID(req.IP)
	a.cfg.Devices = append(a.cfg.Devices, store.DeviceConfig{ID: id, IP: req.IP})
	if a.configPath != "" {
		store.Save(a.configPath, a.cfg)
	}
	sess := device.SessionConfig{ID: id, IP: req.IP}
	if _, _, err := net.SplitHostPort(req.IP); err == nil {
		// 带端口时直接用原地址连接，与 probe 保持一致。
		sess.Addr = req.IP
	}
	a.mgr.Add(sess)
	writeJSON(w, http.StatusCreated, deviceDTO{ID: id, IP: req.IP, Online: true, Name: name})
}

func (a *api) remove(w http.ResponseWriter, id string) {
	a.mgr.Remove(id)
	kept := a.cfg.Devices[:0]
	for _, d := range a.cfg.Devices {
		if d.ID != id {
			kept = append(kept, d)
		}
	}
	a.cfg.Devices = kept
	if a.configPath != "" {
		store.Save(a.configPath, a.cfg)
	}
	w.WriteHeader(http.StatusNoContent)
}

// probeDevice 连 27183 读 dummy+64B 设备名，用于添加前验证设备在线。
func probeDevice(ip string) (string, error) {
	addr := ip
	if _, _, err := net.SplitHostPort(ip); err != nil {
		addr = net.JoinHostPort(ip, "27183")
	}
	conn, err := net.DialTimeout("tcp", addr, 2*time.Second)
	if err != nil {
		return "", err
	}
	defer conn.Close()
	var dummy [1]byte
	if _, err := io.ReadFull(conn, dummy[:]); err != nil {
		return "", err
	}
	var name [device.DeviceNameLen]byte
	if _, err := io.ReadFull(conn, name[:]); err != nil {
		return "", err
	}
	return strings.TrimRight(string(name[:]), "\x00"), nil
}

func sanitizeID(ip string) string {
	return strings.ReplaceAll(ip, ":", "-")
}

func writeJSON(w http.ResponseWriter, code int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	json.NewEncoder(w).Encode(v)
}
