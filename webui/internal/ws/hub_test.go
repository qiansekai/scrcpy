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

// TestUnsubscribeKeepsSameIDConn: two conns share connID "c1" (like two
// browser tabs). Closing one must not remove the other's subscription.
func TestUnsubscribeKeepsSameIDConn(t *testing.T) {
	h := NewHub()
	a := &fakeConn{out: make(chan []byte, 16)}
	b := &fakeConn{out: make(chan []byte, 16)}
	h.subscribe("dev1", a)
	h.subscribe("dev1", b) // same ID, replaces a in the map

	h.unsubscribe("dev1", a) // identity check: must leave b's entry

	h.PublishFrame("dev1", &device.VideoFrame{Data: []byte{0x01}})
	select {
	case <-b.out:
	case <-time.After(time.Second):
		t.Fatal("b lost frames after a disconnected")
	}
	select {
	case x := <-a.out:
		t.Fatalf("a got frame after unsubscribe: %x", x)
	default:
	}
}

// TestSubscribeReplaysCachedConfig: a browser connecting after the one-time
// config frame was published must receive the cached session meta then the
// cached config frame, before any live frame.
func TestSubscribeReplaysCachedConfig(t *testing.T) {
	h := NewHub()
	live := &fakeConn{out: make(chan []byte, 16)}
	h.subscribe("dev1", live)

	h.PublishSession("dev1", device.SessionInfo{Width: 1080, Height: 2400})
	h.PublishFrame("dev1", &device.VideoFrame{Config: true, Data: []byte{0x00, 0x00, 0x00, 0x01, 0x67}})
	for len(live.out) > 0 {
		<-live.out
	}

	late := &fakeConn{out: make(chan []byte, 16)}
	h.subscribe("dev1", late)

	read := func() ([]byte, bool) {
		select {
		case b := <-late.out:
			return b, true
		case <-time.After(time.Second):
			return nil, false
		}
	}

	b, ok := read()
	if !ok {
		t.Fatal("late conn got no replayed session")
	}
	if got := string(b); got != `{"type":"session","width":1080,"height":2400,"codec":"h264"}` {
		t.Fatalf("replayed session = %s", got)
	}
	b, ok = read()
	if !ok {
		t.Fatal("late conn got no replayed config frame")
	}
	if b[0] != 0x01 || b[1]&0x01 == 0 {
		t.Fatalf("replayed config frame = %x", b)
	}
	if len(b) != 2+5 || b[2] != 0x00 {
		t.Fatalf("replayed config payload = %x", b)
	}

	h.PublishFrame("dev1", &device.VideoFrame{Data: []byte{0x65}})
	b, ok = read()
	if !ok {
		t.Fatal("no live frame after replay")
	}
	if b[0] != 0x01 || b[1]&0x01 != 0 {
		t.Fatalf("live frame = %x, want it after replay", b)
	}
}

// TestSubscribeReplaysKeyframe: a late subscriber must receive the cached
// session → config → keyframe preamble (in that order) so it can decode
// immediately instead of waiting for the device's next keyframe.
func TestSubscribeReplaysKeyframe(t *testing.T) {
	h := NewHub()
	live := &fakeConn{out: make(chan []byte, 16)}
	h.subscribe("dev1", live)

	h.PublishSession("dev1", device.SessionInfo{Width: 1080, Height: 2400})
	h.PublishFrame("dev1", &device.VideoFrame{Config: true, Data: []byte{0x67}})
	h.PublishFrame("dev1", &device.VideoFrame{KeyFrame: true, Data: []byte{0x65}})
	for len(live.out) > 0 {
		<-live.out
	}

	late := &fakeConn{out: make(chan []byte, 16)}
	h.subscribe("dev1", late)

	want := [][]byte{
		[]byte(`{"type":"session","width":1080,"height":2400,"codec":"h264"}`),
		{0x01, 0x01, 0x67},
		{0x01, 0x02, 0x65},
	}
	for i, w := range want {
		select {
		case b := <-late.out:
			if string(b) != string(w) {
				t.Fatalf("replay[%d] = %x, want %x", i, b, w)
			}
		case <-time.After(time.Second):
			t.Fatalf("late conn missing replay[%d]", i)
		}
	}
}

// TestNewSessionClearsStaleCache: a new session (reconnect/rotation) must clear
// the previous stream's cached config/keyframe so a late subscriber never gets
// stale parameter sets paired with the new session.
func TestNewSessionClearsStaleCache(t *testing.T) {
	h := NewHub()
	live := &fakeConn{out: make(chan []byte, 16)}
	h.subscribe("dev1", live)

	h.PublishSession("dev1", device.SessionInfo{Width: 1080, Height: 2400})
	h.PublishFrame("dev1", &device.VideoFrame{Config: true, Data: []byte{0x67}})
	h.PublishFrame("dev1", &device.VideoFrame{KeyFrame: true, Data: []byte{0x65}})
	for len(live.out) > 0 {
		<-live.out
	}

	h.PublishSession("dev1", device.SessionInfo{Width: 720, Height: 1600})
	for len(live.out) > 0 {
		<-live.out
	}

	late := &fakeConn{out: make(chan []byte, 16)}
	h.subscribe("dev1", late)

	select {
	case b := <-late.out:
		if got := string(b); got != `{"type":"session","width":720,"height":1600,"codec":"h264"}` {
			t.Fatalf("replayed = %s", got)
		}
	case <-time.After(time.Second):
		t.Fatal("no replayed session")
	}
	select {
	case b := <-late.out:
		t.Fatalf("stale frame leaked after new session: %x", b)
	default:
	}
}
