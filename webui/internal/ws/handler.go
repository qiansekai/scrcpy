package ws

import (
	"context"
	"encoding/json"
	"log"
	"net"
	"net/http"
	"strings"
	"time"

	"github.com/coder/websocket"

	"scrcpy-lan/webui/internal/control"
	"scrcpy-lan/webui/internal/device"
)

// CtrlMsg is a browser -> Go control message. X/Y are device-pixel coordinates
// already scaled by the frontend.
type CtrlMsg struct {
	Type    string  `json:"type"`
	X       float64 `json:"x,omitempty"`
	Y       float64 `json:"y,omitempty"`
	ScreenW int     `json:"screenW,omitempty"`
	ScreenH int     `json:"screenH,omitempty"`
	Action  int     `json:"action,omitempty"`
	Keycode int     `json:"keycode,omitempty"`
	Text    string  `json:"text,omitempty"`
	Clipboard string `json:"clipboard,omitempty"`
	CopyKey   int    `json:"copyKey,omitempty"`
	Paste     bool   `json:"paste,omitempty"`
}

type Handler struct {
	hub     *Hub
	manager *device.Manager
	ctrl    *control.Writer
	origins []string
}

func NewHandler(hub *Hub, manager *device.Manager) *Handler {
	return &Handler{hub: hub, manager: manager, ctrl: &control.Writer{}, origins: defaultOriginPatterns()}
}

// defaultOriginPatterns 允许 localhost/回环 + 本机所有 LAN IPv4 地址，
// 使同网段浏览器经 http://<LAN-ip>:8080 访问时 WS 握手不被 origin 校验拒绝。
func defaultOriginPatterns() []string {
	patterns := []string{"localhost:*", "127.0.0.1:*"}
	addrs, err := net.InterfaceAddrs()
	if err != nil {
		return patterns
	}
	for _, a := range addrs {
		ipnet, ok := a.(*net.IPNet)
		if !ok {
			continue
		}
		ip := ipnet.IP
		if ip.IsLoopback() || ip.To4() == nil {
			continue
		}
		patterns = append(patterns, ip.String()+":*")
	}
	return patterns
}

type connWrapper struct {
	c  *websocket.Conn
	id string
	ws chan []byte
}

func (cw *connWrapper) ID() string { return cw.id }

// Write drops the message when the browser is too slow to keep up.
func (cw *connWrapper) Write(b []byte) {
	select {
	case cw.ws <- b:
	default:
	}
}

func (h *Handler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	deviceID := strings.TrimPrefix(r.URL.Path, "/ws/")
	if deviceID == "" {
		http.Error(w, "missing device id", http.StatusBadRequest)
		return
	}
	// Origin 白名单：仅允许本机回环 + 本机 LAN 地址发起的连接，挡住跨站页面对
	// WS 的 CSRF 注入。REST 无此限制，远程访问仅影响 WS。
	c, err := websocket.Accept(w, r, &websocket.AcceptOptions{
		OriginPatterns: h.origins,
	})
	if err != nil {
		return
	}
	defer c.Close(websocket.StatusNormalClosure, "")

	cw := &connWrapper{c: c, id: "browser", ws: make(chan []byte, 512)}
	h.hub.subscribe(deviceID, cw)
	defer h.hub.unsubscribe(deviceID, cw)

	ctx := r.Context()

	// Write goroutine: hub pushes -> browser. Video (0x01) and audio (0x02)
	// frames go out as binary; JSON session/clipboard messages go out as text.
	go func() {
		for b := range cw.ws {
			writeCtx, cancel := context.WithTimeout(ctx, 2*time.Second)
			typ := websocket.MessageBinary
			if b[0] != 0x01 && b[0] != 0x02 {
				typ = websocket.MessageText
			}
			err := c.Write(writeCtx, typ, b)
			cancel()
			if err != nil {
				return
			}
		}
	}()
	defer close(cw.ws)

	// Read loop: browser control messages -> device control socket.
	for {
		_, data, err := c.Read(ctx)
		if err != nil {
			return
		}
		var m CtrlMsg
		if err := json.Unmarshal(data, &m); err != nil {
			continue
		}
		h.route(deviceID, m)
	}
}

// route translates one browser control message into scrcpy control bytes and
// hands it to the device session.
func (h *Handler) route(deviceID string, m CtrlMsg) {
	var b []byte
	switch m.Type {
	case "touch":
		b = h.ctrl.InjectTouch(control.Touch{
			Action:    m.Action,
			PointerID: 0,
			X:         int32(m.X),
			Y:         int32(m.Y),
			ScreenW:   uint16(m.ScreenW),
			ScreenH:   uint16(m.ScreenH),
			Pressure:  0xFFFF,
		})
	case "key":
		b = h.ctrl.InjectKey(control.Key{Action: m.Action, Keycode: int32(m.Keycode)})
	case "text":
		b = h.ctrl.InjectText(m.Text)
	case "getClipboard":
		b = h.ctrl.GetClipboard(byte(m.CopyKey))
	case "setClipboard":
		b = h.ctrl.SetClipboard(m.Clipboard, m.Paste, 0)
	case "home":
		// KEYCODE_HOME = 3
		b = append(h.ctrl.InjectKey(control.Key{Action: control.KeyActionDown, Keycode: 3}),
			h.ctrl.InjectKey(control.Key{Action: control.KeyActionUp, Keycode: 3})...)
	case "back":
		b = append(h.ctrl.InjectKey(control.Key{Action: control.KeyActionDown, Keycode: 4}),
			h.ctrl.InjectKey(control.Key{Action: control.KeyActionUp, Keycode: 4})...)
	case "recents":
		// KEYCODE_APP_SWITCH = 187
		b = append(h.ctrl.InjectKey(control.Key{Action: control.KeyActionDown, Keycode: 187}),
			h.ctrl.InjectKey(control.Key{Action: control.KeyActionUp, Keycode: 187})...)
	case "power":
		b = h.ctrl.InjectKey(control.Key{Action: control.KeyActionDown, Keycode: 26})
	case "rotate":
		b = h.ctrl.RotateDevice()
	default:
		log.Printf("unknown control msg type %q", m.Type)
		return
	}
	if err := h.manager.SendControl(deviceID, b); err != nil {
		log.Printf("send control to %s: %v", deviceID, err)
	}
}
