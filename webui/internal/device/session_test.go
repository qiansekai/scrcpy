package device

import (
	"bytes"
	"encoding/binary"
	"io"
	"net"
	"sync"
	"testing"
	"time"

	"scrcpy-lan/webui/internal/control"
)

// fakeDevice 模拟设备端 27183：按 video→audio→control 顺序 accept 三个连接。
type fakeDevice struct {
	ln net.Listener
}

func startFakeDevice(t *testing.T) *fakeDevice {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	fd := &fakeDevice{ln: ln}
	go fd.serve()
	t.Cleanup(func() { ln.Close() })
	return fd
}

func (fd *fakeDevice) serve() {
	// video
	video, err := fd.ln.Accept()
	if err != nil {
		return
	}
	defer video.Close()
	video.Write([]byte{0x00}) // dummy
	name := make([]byte, DeviceNameLen)
	copy(name, "FakePhone")
	video.Write(name)
	video.Write(CodecH264[:])
	writeSessionAndFrames(video)
	// audio
	audio, err := fd.ln.Accept()
	if err != nil {
		return
	}
	defer audio.Close()
	audio.Write([]byte{'o', 'p', 'u', 's'})
	// control
	controlConn, err := fd.ln.Accept()
	if err != nil {
		return
	}
	defer controlConn.Close()
	// 回一个剪贴板消息，验证 reader 通路
	var msg bytes.Buffer
	msg.WriteByte(control.DevMsgClipboard)
	binary.Write(&msg, binary.BigEndian, uint32(2))
	msg.WriteString("hi")
	controlConn.Write(msg.Bytes())
	// 保持连接直到测试结束
	io.Copy(io.Discard, controlConn)
}

func writeSessionAndFrames(w io.Writer) {
	binary.Write(w, binary.BigEndian, uint32(0x80000000))
	binary.Write(w, binary.BigEndian, uint32(1080))
	binary.Write(w, binary.BigEndian, uint32(2400))
	writeOneFrame(w, 0x4000000000000000, []byte{0x00, 0x00, 0x00, 0x01, 0x67})
	writeOneFrame(w, 1000|0x2000000000000000, []byte{0x00, 0x00, 0x00, 0x01, 0x65})
}

func writeOneFrame(w io.Writer, ptsAndFlags uint64, payload []byte) {
	var h [8]byte
	binary.BigEndian.PutUint64(h[:], ptsAndFlags)
	w.Write(h[:])
	binary.Write(w, binary.BigEndian, uint32(len(payload)))
	w.Write(payload)
}

// recordingBroadcaster 被会话内部 goroutine 与测试主 goroutine 并发访问，需加锁。
type recordingBroadcaster struct {
	mu      sync.Mutex
	frames  []*VideoFrame
	devices []string
	session []SessionInfo
	msgs    []*control.DeviceMessage
}

func (r *recordingBroadcaster) PublishSession(id string, s SessionInfo) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.devices = append(r.devices, id)
	r.session = append(r.session, s)
}
func (r *recordingBroadcaster) PublishFrame(id string, f *VideoFrame) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.devices = append(r.devices, id)
	r.frames = append(r.frames, f)
}
func (r *recordingBroadcaster) PublishDeviceMessage(id string, m *control.DeviceMessage) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.msgs = append(r.msgs, m)
}

func (r *recordingBroadcaster) frameCount() int {
	r.mu.Lock()
	defer r.mu.Unlock()
	return len(r.frames)
}
func (r *recordingBroadcaster) firstDevice() string {
	r.mu.Lock()
	defer r.mu.Unlock()
	if len(r.devices) == 0 {
		return ""
	}
	return r.devices[0]
}
func (r *recordingBroadcaster) firstSession() (SessionInfo, bool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if len(r.session) == 0 {
		return SessionInfo{}, false
	}
	return r.session[0], true
}
func (r *recordingBroadcaster) firstMsg() *control.DeviceMessage {
	r.mu.Lock()
	defer r.mu.Unlock()
	if len(r.msgs) == 0 {
		return nil
	}
	return r.msgs[0]
}

func TestSessionConnectsAndStreams(t *testing.T) {
	fd := startFakeDevice(t)
	rec := &recordingBroadcaster{}
	cfg := SessionConfig{ID: "dev1", Addr: fd.ln.Addr().String()}
	sess := NewStreamSession(cfg, rec)
	sess.Start()
	defer sess.Stop()

	deadline := time.Now().Add(3 * time.Second)
	for rec.frameCount() < 2 && time.Now().Before(deadline) {
		time.Sleep(10 * time.Millisecond)
	}
	if rec.frameCount() != 2 {
		t.Fatalf("frames = %d, want 2", rec.frameCount())
	}
	if rec.firstDevice() != "dev1" {
		t.Fatalf("no session published: %v", rec.devices)
	}
	sessInfo, ok := rec.firstSession()
	if !ok || sessInfo.Width != 1080 {
		t.Fatalf("session = %+v", sessInfo)
	}
	if sess.Name() != "FakePhone" {
		t.Fatalf("name = %q", sess.Name())
	}
	if m := rec.firstMsg(); m == nil || m.Text != "hi" {
		t.Fatalf("device msgs = %+v", rec.msgs)
	}
}
