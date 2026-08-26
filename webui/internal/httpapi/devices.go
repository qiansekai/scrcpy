package httpapi

import (
	"encoding/json"
	"io"
	"log"
	"net"
	"net/http"
	"strings"
	"sync"
	"time"

	"scrcpy-lan/webui/internal/device"
	"scrcpy-lan/webui/internal/status"
	"scrcpy-lan/webui/internal/store"
)

type api struct {
	cfg        *store.Config
	mgr        *device.Manager
	configPath string
	collector  *status.Collector
}

type deviceDTO struct {
	ID      string `json:"id"`
	IP      string `json:"ip"`
	Online  bool   `json:"online"`
	Name    string `json:"name,omitempty"`
	Battery int    `json:"battery,omitempty"`
	Plugged bool   `json:"plugged,omitempty"`
	Model   string `json:"model,omitempty"`
	Android string `json:"android,omitempty"`
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
	rest := strings.TrimPrefix(r.URL.Path, "/api/devices/")
	if strings.HasSuffix(rest, "/exec") {
		a.exec(w, r, strings.TrimSuffix(rest, "/exec"))
		return
	}
	if strings.HasSuffix(rest, "/status") {
		a.status(w, r, strings.TrimSuffix(rest, "/status"))
		return
	}
	if r.Method != http.MethodDelete {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	a.remove(w, rest)
}

// status 返回单台设备的最近一次状态快照（无缓存则即时采样）。
func (a *api) status(w http.ResponseWriter, r *http.Request, id string) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if a.collector == nil {
		http.Error(w, "collector not available", http.StatusServiceUnavailable)
		return
	}
	info, ok := a.collector.Snapshot(id)
	if !ok {
		http.Error(w, "no such device", http.StatusNotFound)
		return
	}
	writeJSON(w, http.StatusOK, info)
}

// exec runs one shell command on the device via the admin channel (27184).
func (a *api) exec(w http.ResponseWriter, r *http.Request, id string) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	var req struct {
		Cmd string `json:"cmd"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.Cmd == "" {
		http.Error(w, "bad request", http.StatusBadRequest)
		return
	}
	res, err := a.mgr.ExecCommand(id, req.Cmd)
	if err != nil {
		http.Error(w, "admin error: "+err.Error(), http.StatusBadGateway)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"exitCode": res.ExitCode, "stdout": res.Stdout, "stderr": res.Stderr})
}

func (a *api) list(w http.ResponseWriter) {
	out := []deviceDTO{}
	snaps := map[string]status.Info{}
	if a.collector != nil {
		for _, s := range a.collector.SnapshotAll() {
			snaps[s.ID] = s
		}
	}
	for _, d := range a.cfg.Devices {
		dto := deviceDTO{ID: d.ID, IP: d.IP, Online: a.mgr.Has(d.ID), Name: a.mgr.Name(d.ID)}
		if s, ok := snaps[d.ID]; ok {
			dto.Battery = s.Battery
			dto.Plugged = s.Plugged
			dto.Model = s.Model
			dto.Android = s.Android
			dto.Online = dto.Online || s.Online
		}
		out = append(out, dto)
	}
	writeJSON(w, http.StatusOK, out)
}

// handleDiscover 扫描本机所在网段（/24），找出 27184 管理通道可达的设备并
// 加入配置（已存在的跳过）。返回新增设备列表。
func (a *api) handleDiscover(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	added := []deviceDTO{}
	existing := map[string]bool{}
	for _, d := range a.cfg.Devices {
		existing[d.ID] = true
	}
	for _, ip := range scanLAN() {
		id := sanitizeID(ip)
		if existing[id] {
			continue
		}
		name, err := probeDevice(ip)
		if err != nil {
			continue
		}
		existing[id] = true
		a.cfg.Devices = append(a.cfg.Devices, store.DeviceConfig{ID: id, IP: ip})
		a.mgr.Add(device.SessionConfig{ID: id, IP: ip})
		if a.collector != nil {
			a.collector.Track([]string{id})
		}
		added = append(added, deviceDTO{ID: id, IP: ip, Online: true, Name: name})
	}
	if len(added) > 0 && a.configPath != "" {
		if err := store.Save(a.configPath, a.cfg); err != nil {
			log.Printf("save config: %v", err)
		}
	}
	writeJSON(w, http.StatusOK, map[string]any{"added": added})
}

// scanLAN 返回本机每个 IPv4 地址所在 /24 网段的全部主机地址（含本机）。
// 并发对 27184 做 TCP 拨号探测，超时 400ms。链路本地（169.254/16）跳过。
func scanLAN() []string {
	addrs, err := net.InterfaceAddrs()
	if err != nil {
		return nil
	}
	subnets := map[string]string{} // 本机 IP -> 掩码后的 /24 网络地址
	for _, a := range addrs {
		ipnet, ok := a.(*net.IPNet)
		if !ok || ipnet.IP.To4() == nil || ipnet.IP.IsLoopback() {
			continue
		}
		ip4 := ipnet.IP.To4()
		if ip4[0] == 169 && ip4[1] == 254 {
			continue // 链路本地地址，扫了也无意义
		}
		masked := make(net.IP, 4)
		copy(masked, ip4)
		subnets[ip4.String()] = masked.Mask(net.CIDRMask(24, 32)).String()
	}

	const probeTimeout = 400 * time.Millisecond
	var mu sync.Mutex
	found := []string{}
	sem := make(chan struct{}, 128)
	var wg sync.WaitGroup
	for _, base := range subnets {
		net4 := net.ParseIP(base).To4()
		for i := 1; i < 255; i++ {
			// 注意：net.IPv4() 返回 16 字节的 IPv4-in-IPv6 形式，必须用
			// make(IP,4) 才能得到纯 4 字节地址。
			ip := make(net.IP, 4)
			copy(ip, net4)
			ip[3] = byte(i)
			candidate := ip.String()
			sem <- struct{}{}
			wg.Add(1)
			go func(ip string) {
				defer wg.Done()
				defer func() { <-sem }()
				conn, err := net.DialTimeout("tcp", net.JoinHostPort(ip, "27184"), probeTimeout)
				if err != nil {
					return
				}
				conn.Close()
				mu.Lock()
				found = append(found, ip)
				mu.Unlock()
			}(candidate)
		}
	}
	wg.Wait()
	return found
}

func (a *api) add(w http.ResponseWriter, r *http.Request) {
	var req struct {
		IP string `json:"ip"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.IP == "" {
		http.Error(w, "bad request", http.StatusBadRequest)
		return
	}
	id := sanitizeID(req.IP)
	for _, d := range a.cfg.Devices {
		if d.ID == id {
			http.Error(w, "device already exists", http.StatusConflict)
			return
		}
	}
	name, err := probeDevice(req.IP)
	if err != nil {
		http.Error(w, "device unreachable: "+err.Error(), http.StatusBadGateway)
		return
	}
	a.cfg.Devices = append(a.cfg.Devices, store.DeviceConfig{ID: id, IP: req.IP})
	if a.configPath != "" {
		if err := store.Save(a.configPath, a.cfg); err != nil {
			log.Printf("save config: %v", err)
		}
	}
	sess := device.SessionConfig{ID: id, IP: req.IP}
	if _, _, err := net.SplitHostPort(req.IP); err == nil {
		// 带端口时直接用原地址连接，与 probe 保持一致。
		sess.Addr = req.IP
	}
	a.mgr.Add(sess)
	if a.collector != nil {
		a.collector.Track([]string{id})
	}
	writeJSON(w, http.StatusCreated, deviceDTO{ID: id, IP: req.IP, Online: true, Name: name})
}

func (a *api) remove(w http.ResponseWriter, id string) {
	a.mgr.Remove(id)
	if a.collector != nil {
		a.collector.Untrack(id)
	}
	kept := a.cfg.Devices[:0]
	for _, d := range a.cfg.Devices {
		if d.ID != id {
			kept = append(kept, d)
		}
	}
	a.cfg.Devices = kept
	if a.configPath != "" {
		if err := store.Save(a.configPath, a.cfg); err != nil {
			log.Printf("save config: %v", err)
		}
	}
	w.WriteHeader(http.StatusNoContent)
}

// probeDevice 连 27183 读 dummy+64B 设备名，用于添加前验证设备在线。
// scrcpy 服务端按 video→audio→control 顺序接受连接，且要等三个连接都建好
// 才写设备名，所以探测必须同样建立三条连接，否则会卡在读设备名上。
func probeDevice(ip string) (string, error) {
	addr := ip
	if _, _, err := net.SplitHostPort(ip); err != nil {
		addr = net.JoinHostPort(ip, "27183")
	}
	dial := func() (net.Conn, error) { return net.DialTimeout("tcp", addr, 2*time.Second) }
	video, err := dial()
	if err != nil {
		return "", err
	}
	defer video.Close()
	audio, err := dial()
	if err != nil {
		return "", err
	}
	defer audio.Close()
	ctl, err := dial()
	if err != nil {
		return "", err
	}
	defer ctl.Close()

	video.SetReadDeadline(time.Now().Add(2 * time.Second))
	var dummy [1]byte
	if _, err := io.ReadFull(video, dummy[:]); err != nil {
		return "", err
	}
	var name [device.DeviceNameLen]byte
	if _, err := io.ReadFull(video, name[:]); err != nil {
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
