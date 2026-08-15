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

// hubState caches the stream preamble (session meta + H.264 SPS/PPS config
// frame + last keyframe + Opus config) so a browser that connects mid-stream
// can decode immediately instead of waiting for the device's next keyframe.
type hubState struct {
	sessionJSON []byte
	configFrame []byte
	keyFrame    []byte
	audioConfig []byte
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
		if st.keyFrame != nil {
			c.Write(st.keyFrame)
		}
		if st.audioConfig != nil {
			c.Write(st.audioConfig)
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
	// 新会话 = 新流，清掉上一个流的 config/keyframe/audio，避免回放陈旧参数。
	h.state[id].sessionJSON = payload
	h.state[id].configFrame = nil
	h.state[id].keyFrame = nil
	h.state[id].audioConfig = nil
	h.broadcastLocked(id, payload)
	h.mu.Unlock()
}

func (h *Hub) PublishAudioFrame(id string, f *device.AudioFrame) {
	out := make([]byte, 2+len(f.Data))
	out[0] = 0x02 // 音频帧
	if f.Config {
		out[1] |= 0x01
	}
	copy(out[2:], f.Data)
	if f.Config {
		h.mu.Lock()
		if h.state[id] == nil {
			h.state[id] = &hubState{}
		}
		h.state[id].audioConfig = out
		h.broadcastLocked(id, out)
		h.mu.Unlock()
		return
	}
	h.broadcast(id, out)
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
	if f.Config || f.KeyFrame {
		h.mu.Lock()
		if h.state[id] == nil {
			h.state[id] = &hubState{}
		}
		if f.Config {
			h.state[id].configFrame = out
		} else {
			h.state[id].keyFrame = out
		}
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
