package ws

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/coder/websocket"

	"scrcpy-lan/webui/internal/control"
	"scrcpy-lan/webui/internal/device"
)

type noopBroadcaster struct{}

func (noopBroadcaster) PublishSession(string, device.SessionInfo)          {}
func (noopBroadcaster) PublishFrame(string, *device.VideoFrame)            {}
func (noopBroadcaster) PublishAudioFrame(string, *device.AudioFrame)       {}
func (noopBroadcaster) PublishDeviceMessage(string, *control.DeviceMessage) {}

func TestHandlerOriginAllowList(t *testing.T) {
	hub := NewHub()
	h := NewHandler(hub, device.NewManager(noopBroadcaster{}))
	srv := httptest.NewServer(h)
	defer srv.Close()

	u := "ws" + strings.TrimPrefix(srv.URL, "http") + "/ws/dev1"
	dial := func(origin string) error {
		ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer cancel()
		c, _, err := websocket.Dial(ctx, u, &websocket.DialOptions{
			HTTPHeader: http.Header{"Origin": []string{origin}},
		})
		if c != nil {
			c.Close(websocket.StatusNormalClosure, "")
		}
		return err
	}

	if err := dial("http://localhost:5173"); err != nil {
		t.Fatalf("vite dev origin rejected: %v", err)
	}
	if err := dial("http://127.0.0.1:5173"); err != nil {
		t.Fatalf("loopback dev origin rejected: %v", err)
	}
	if err := dial("http://evil.example.com"); err == nil {
		t.Fatal("foreign origin accepted")
	}
}
