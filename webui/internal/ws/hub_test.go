package ws

import (
	"testing"
	"time"

	"scrcpy-lan/webui/internal/control"
	"scrcpy-lan/webui/internal/device"
)

// fakeConn implements the hub's conn interface: a one-way write channel.
type fakeConn struct{ out chan []byte }

func (c *fakeConn) Write(b []byte) { c.out <- b }
func (c *fakeConn) ID() string     { return "c1" }

func TestHubFanout(t *testing.T) {
	h := NewHub()
	c := &fakeConn{out: make(chan []byte, 16)}
	h.subscribe("dev1", c)

	h.PublishFrame("dev1", &device.VideoFrame{Pts: 5, Data: []byte{0x01, 0x02}})
	select {
	case b := <-c.out:
		if b[0] != 0x01 {
			t.Fatalf("first byte = %x, want 0x01", b[0])
		}
	case <-time.After(time.Second):
		t.Fatal("no frame delivered")
	}

	h.unsubscribe("dev1", c)
	h.PublishFrame("dev1", &device.VideoFrame{Data: []byte{0xFF}})
	select {
	case b := <-c.out:
		t.Fatalf("got frame after unsubscribe: %x", b)
	default:
	}
}

func TestHubIgnoresOtherDevices(t *testing.T) {
	h := NewHub()
	c := &fakeConn{out: make(chan []byte, 16)}
	h.subscribe("dev1", c)
	h.PublishFrame("dev2", &device.VideoFrame{Data: []byte{0xAA}})
	select {
	case b := <-c.out:
		t.Fatalf("dev2 frame leaked to dev1 sub: %x", b)
	case <-time.After(50 * time.Millisecond):
	}
}

func TestHubJSONMessageShapes(t *testing.T) {
	h := NewHub()
	c := &fakeConn{out: make(chan []byte, 16)}
	h.subscribe("dev1", c)

	h.PublishSession("dev1", device.SessionInfo{Width: 1080, Height: 2400})
	h.PublishDeviceMessage("dev1", &control.DeviceMessage{Type: control.DevMsgClipboard, Text: `hi "there"`})

	if got := string(<-c.out); got != `{"type":"session","width":1080,"height":2400,"codec":"h264"}` {
		t.Fatalf("session = %s", got)
	}
	if got := string(<-c.out); got != `{"type":"clipboard","text":"hi \"there\""}` {
		t.Fatalf("clipboard = %s", got)
	}

	h.PublishDeviceMessage("dev1", &control.DeviceMessage{Type: control.DevMsgUhidOutput})
	select {
	case b := <-c.out:
		t.Fatalf("non-clipboard message forwarded: %s", b)
	case <-time.After(50 * time.Millisecond):
	}
}

func TestPublishFrameCopiesPayload(t *testing.T) {
	h := NewHub()
	c := &fakeConn{out: make(chan []byte, 16)}
	h.subscribe("dev1", c)

	f := &device.VideoFrame{Data: []byte{0xAA}}
	h.PublishFrame("dev1", f)
	f.Data[0] = 0xBB // parser reuses its internal buffer; hub must not alias it

	select {
	case b := <-c.out:
		if b[2] != 0xAA {
			t.Fatalf("frame payload = %x, want copied original 0xAA", b[2])
		}
	case <-time.After(time.Second):
		t.Fatal("no frame delivered")
	}
}
