package ws

import (
	"encoding/json"
	"strconv"
	"sync"

	"scrcpy-lan/webui/internal/control"
	"scrcpy-lan/webui/internal/device"
)

// conn is the hub's minimal abstraction of a browser connection (provided by the handler).
type conn interface {
	Write(b []byte)
	ID() string
}

// hubState caches the one-time stream preamble (session meta + H.264 SPS/PPS
// config frame) so a browser that connects mid-stream can still decode.
type hubState struct {
	sessionJSON []byte
	configFrame []byte
}

type Hub struct {
	mu    sync.RWMutex
	subs  map[string]map[string]conn // deviceID -> connID -> conn
	state map[string]*hubState       // deviceID -> cached session/config
	ctrl  *control.Writer
}

func NewHub() *Hub {
	return &Hub{
		subs:  make(map[string]map[string]conn),
		state: make(map[string]*hubState),
		ctrl:  &control.Writer{},
	}
}

func (h *Hub) subscribe(deviceID string, c conn) {
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.subs[deviceID] == nil {
		h.subs[deviceID] = make(map[string]conn)
	}
	h.subs[deviceID][c.ID()] = c
	if st := h.state[deviceID]; st != nil {
		if st.sessionJSON != nil {
			c.Write(st.sessionJSON)
		}
		if st.configFrame != nil {
			c.Write(st.configFrame)
		}
	}
}

func (h *Hub) unsubscribe(deviceID string, c conn) {
	h.mu.Lock()
	defer h.mu.Unlock()
	if m := h.subs[deviceID]; m != nil && m[c.ID()] == c {
		delete(m, c.ID())
	}
}

func (h *Hub) PublishSession(id string, s device.SessionInfo) {
	payload := append([]byte(`{"type":"session","width":`), []byte(itoa(s.Width))...)
	payload = append(payload, []byte(`,"height":`+itoa(s.Height)+`,"codec":"h264"}`)...)
	h.mu.Lock()
	if h.state[id] == nil {
		h.state[id] = &hubState{}
	}
	h.state[id].sessionJSON = payload
	h.broadcastLocked(id, payload)
	h.mu.Unlock()
}

func (h *Hub) PublishFrame(id string, f *device.VideoFrame) {
	out := make([]byte, 2+len(f.Data))
	out[0] = 0x01
	if f.Config {
		out[1] |= 0x01
	}
	if f.KeyFrame {
		out[1] |= 0x02
	}
	copy(out[2:], f.Data)
	if f.Config {
		h.mu.Lock()
		if h.state[id] == nil {
			h.state[id] = &hubState{}
		}
		h.state[id].configFrame = out
		h.broadcastLocked(id, out)
		h.mu.Unlock()
		return
	}
	h.broadcast(id, out)
}

func (h *Hub) PublishDeviceMessage(id string, m *control.DeviceMessage) {
	if m.Type != control.DevMsgClipboard {
		return
	}
	payload := []byte(`{"type":"clipboard","text":` + jsonQuote(m.Text) + `}`)
	h.broadcast(id, payload)
}

func (h *Hub) broadcast(deviceID string, b []byte) {
	h.mu.RLock()
	defer h.mu.RUnlock()
	h.broadcastLocked(deviceID, b)
}

// broadcastLocked fans out to one device's conns; the caller holds h.mu.
func (h *Hub) broadcastLocked(deviceID string, b []byte) {
	for _, c := range h.subs[deviceID] {
		c.Write(b)
	}
}

func itoa(v int) string { return strconv.Itoa(v) }

func jsonQuote(s string) string {
	b, _ := json.Marshal(s)
	return string(b)
}
