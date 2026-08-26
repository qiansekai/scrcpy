package device

import (
	"io"
	"net"
	"strconv"
	"sync"
	"sync/atomic"
	"time"

	"scrcpy-lan/webui/internal/control"
)

const defaultVideoPort = 27183

// teeWriter 把写入同时转给当前 tee 目标（默认丢弃）。用于把设备的原始
// 视频/音频/控制字节分流给原生 scrcpy 客户端，而不影响 web 的解析路径。
type teeWriter struct {
	mu sync.RWMutex
	w  io.Writer
}

func newTeeWriter() *teeWriter {
	return &teeWriter{w: io.Discard}
}

func (t *teeWriter) Set(w io.Writer) {
	t.mu.Lock()
	t.w = w
	t.mu.Unlock()
}

func (t *teeWriter) Write(p []byte) (int, error) {
	t.mu.RLock()
	w := t.w
	t.mu.RUnlock()
	n, err := w.Write(p)
	if err != nil {
		t.Set(io.Discard) // 原生端断开，停 tee，web 不受影响
	}
	return n, err
}

// teeReader 读 src 时把读到的字节同时写给 tee。
type teeReader struct {
	src io.Reader
	tee *teeWriter
}

func (r *teeReader) Read(p []byte) (int, error) {
	n, err := r.src.Read(p)
	if n > 0 {
		r.tee.Write(p[:n])
	}
	return n, err
}

type SessionConfig struct {
	ID        string
	IP        string
	Addr      string
	VideoPort int
}

type Broadcaster interface {
	PublishSession(id string, s SessionInfo)
	PublishFrame(id string, f *VideoFrame)
	PublishAudioFrame(id string, f *AudioFrame)
	PublishDeviceMessage(id string, m *control.DeviceMessage)
}

type StreamSession struct {
	cfg  SessionConfig
	b    Broadcaster
	ctrl chan []byte
	stop chan struct{}
	done chan struct{}
	name string
	mu   sync.RWMutex

	stopOnce sync.Once

	// 原生客户端桥接（AttachNative 用）
	videoHeader []byte
	audioHeader []byte
	videoTee    *teeWriter
	audioTee    *teeWriter
	controlTee  *teeWriter
	headerOnce  sync.Once
	headerReady chan struct{}
}

func NewStreamSession(cfg SessionConfig, b Broadcaster) *StreamSession {
	if cfg.VideoPort == 0 {
		cfg.VideoPort = defaultVideoPort
	}
	return &StreamSession{
		cfg:         cfg,
		b:           b,
		ctrl:        make(chan []byte, 256),
		stop:        make(chan struct{}),
		done:        make(chan struct{}),
		videoTee:    newTeeWriter(),
		audioTee:    newTeeWriter(),
		controlTee:  newTeeWriter(),
		headerReady: make(chan struct{}),
	}
}

func (s *StreamSession) Start() { go s.run() }

// Stop 幂等：可重复调用，第一次关闭 stop 并等待运行协程退出。
func (s *StreamSession) Stop() {
	s.stopOnce.Do(func() { close(s.stop) })
	<-s.done
}

func (s *StreamSession) Name() string {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.name
}

// SendControl 把控制消息排入写队列。断线重连期间队列可能满，此时直接丢弃
// 而非阻塞调用方（hub 的写路径不允许被卡住）；重连后输入自然恢复。
func (s *StreamSession) SendControl(b []byte) {
	select {
	case s.ctrl <- b:
	default:
	}
}

func (s *StreamSession) run() {
	defer close(s.done)
	backoff := time.Second
	for {
		select {
		case <-s.stop:
			return
		default:
		}
		err := s.connectOnce()
		if err == nil {
			return
		}
		select {
		case <-s.stop:
			return
		case <-time.After(backoff):
		}
		backoff *= 2
		if backoff > 30*time.Second {
			backoff = 30 * time.Second
		}
	}
}

func (s *StreamSession) connectOnce() error {
	addr := net.JoinHostPort(s.cfg.IP, strconv.Itoa(s.cfg.VideoPort))
	if s.cfg.Addr != "" {
		addr = s.cfg.Addr
	}

	// 拨号必须带超时：对离线设备的无超时 Dial 会阻塞几十秒，拖住 Stop/Remove。
	dial := func() (net.Conn, error) {
		return net.DialTimeout("tcp", addr, 5*time.Second)
	}
	video, err := dial()
	if err != nil {
		return err
	}
	defer video.Close()
	audio, err := dial()
	if err != nil {
		return err
	}
	defer audio.Close()
	ctl, err := dial()
	if err != nil {
		return err
	}
	defer ctl.Close()

	vs := NewVideoStream(&teeReader{src: video, tee: s.videoTee})
	if err := vs.ReadMeta(); err != nil {
		return err
	}
	s.mu.Lock()
	s.name = vs.Device
	s.videoHeader = append([]byte(nil), vs.StreamHeader()...)
	s.mu.Unlock()

	// Reader errors propagate to the writer so a device disconnect on a quiet
	// control channel still triggers reconnect.
	connErr := make(chan error, 3)
	fail := func(err error) {
		select {
		case connErr <- err:
		default:
		}
	}

	// audio reader: parse and forward instead of discarding, so browsers get sound.
	go func() {
		as := NewAudioStream(&teeReader{src: audio, tee: s.audioTee})
		if err := as.ReadMeta(); err != nil {
			fail(err)
			return
		}
		s.mu.Lock()
		s.audioHeader = append([]byte(nil), as.StreamHeader()...)
		s.mu.Unlock()
		for {
			f, err := as.Next()
			if err != nil {
				fail(err)
				return
			}
			s.b.PublishAudioFrame(s.cfg.ID, f)
		}
	}()

	// video reader.
	go func() {
		for {
			f, sess, err := vs.Next()
			if err != nil {
				fail(err)
				return
			}
			if sess != nil {
				// 缓存 session meta 到流头，原生客户端桥接需要完整头。
				s.mu.Lock()
				s.videoHeader = append(s.videoHeader, vs.LastHeader[:]...)
				s.mu.Unlock()
				s.headerOnce.Do(func() { close(s.headerReady) })
				s.b.PublishSession(s.cfg.ID, *sess)
				continue
			}
			s.b.PublishFrame(s.cfg.ID, f)
		}
	}()

	// device->client reader.
	go func() {
		cr := control.NewReader(&teeReader{src: ctl, tee: s.controlTee})
		for {
			m, err := cr.Read()
			if err != nil {
				fail(err)
				return
			}
			s.b.PublishDeviceMessage(s.cfg.ID, m)
		}
	}()

	// control writer: the loop that keeps the connection alive.
	for {
		select {
		case <-s.stop:
			return nil
		case err := <-connErr:
			return err
		case b := <-s.ctrl:
			if _, err := ctl.Write(b); err != nil {
				return err
			}
		}
	}
}

type Manager struct {
	b        Broadcaster
	mu       sync.RWMutex
	sessions map[string]*StreamSession
}

func NewManager(b Broadcaster) *Manager {
	return &Manager{b: b, sessions: make(map[string]*StreamSession)}
}

func (m *Manager) Add(cfg SessionConfig) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if _, ok := m.sessions[cfg.ID]; ok {
		return
	}
	sess := NewStreamSession(cfg, m.b)
	m.sessions[cfg.ID] = sess
	sess.Start()
}

func (m *Manager) Remove(id string) {
	m.mu.Lock()
	sess, ok := m.sessions[id]
	if ok {
		delete(m.sessions, id)
	}
	m.mu.Unlock()
	if ok {
		sess.Stop()
	}
}

func (m *Manager) Has(id string) bool {
	m.mu.RLock()
	defer m.mu.RUnlock()
	_, ok := m.sessions[id]
	return ok
}

func (m *Manager) Name(id string) string {
	m.mu.RLock()
	defer m.mu.RUnlock()
	sess, ok := m.sessions[id]
	if !ok {
		return ""
	}
	return sess.Name()
}

func (m *Manager) SendControl(id string, b []byte) error {
	m.mu.RLock()
	sess, ok := m.sessions[id]
	m.mu.RUnlock()
	if !ok {
		return io.ErrClosedPipe
	}
	sess.SendControl(b)
	return nil
}

func (m *Manager) Session(id string) *StreamSession {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.sessions[id]
}

// AdminAddr 返回设备管理通道（27184）的 host:port 地址，供批量任务等直接
// 建连使用。Addr 带端口时只取主机名部分。
func (m *Manager) AdminAddr(id string) (string, error) {
	m.mu.RLock()
	sess, ok := m.sessions[id]
	m.mu.RUnlock()
	if !ok {
		return "", io.ErrClosedPipe
	}
	host := sess.cfg.IP
	if sess.cfg.Addr != "" {
		host = sess.cfg.Addr
	}
	if h, _, err := net.SplitHostPort(host); err == nil {
		host = h
	}
	return net.JoinHostPort(host, strconv.Itoa(defaultAdminPort)), nil
}

// First 返回任意一个活动会话（单设备场景用），无则 nil。
func (m *Manager) First() *StreamSession {
	m.mu.RLock()
	defer m.mu.RUnlock()
	for _, s := range m.sessions {
		return s
	}
	return nil
}

// chanWriter 把字节缓冲后写入原生客户端连接（非阻塞，缓冲满则丢帧，
// 实时视频/音频可接受少量缺帧）。
type chanWriter struct {
	ch   chan []byte
	done atomic.Bool
}

func (cw *chanWriter) Write(p []byte) (int, error) {
	if cw.done.Load() {
		return 0, io.ErrClosedPipe
	}
	b := make([]byte, len(p))
	copy(b, p)
	select {
	case cw.ch <- b:
		return len(p), nil
	default:
		return len(p), nil // 缓冲满，丢帧
	}
}

func (cw *chanWriter) run(c net.Conn) {
	for b := range cw.ch {
		if _, err := c.Write(b); err != nil {
			cw.done.Store(true)
			return
		}
	}
}

func newConnWriter(c net.Conn) io.Writer {
	cw := &chanWriter{ch: make(chan []byte, 256)}
	go cw.run(c)
	return cw
}

// AttachNative 把原生 scrcpy 客户端的三路连接桥接到本设备会话：先重放
// 流头（dummy+name+codec+session），之后设备原始字节经 tee 分流给原生
// 客户端；原生客户端的控制消息转发给设备。断开时 tee 自动降级为丢弃，
// 不影响 web 订阅者。
func (s *StreamSession) AttachNative(video, audio, control net.Conn) {
	select {
	case <-s.headerReady:
	case <-s.stop:
		return
	}
	s.mu.RLock()
	vh := s.videoHeader
	ah := s.audioHeader
	s.mu.RUnlock()

	video.Write(vh[1:]) // 跳过 dummy（代理 accept video 时已发）
	audio.Write(ah)
	s.videoTee.Set(newConnWriter(video))
	s.audioTee.Set(newConnWriter(audio))
	s.controlTee.Set(newConnWriter(control))

	// 原生客户端 -> 设备：控制消息原样转发。
	go func() {
		buf := make([]byte, 4096)
		for {
			n, err := control.Read(buf)
			if err != nil {
				return
			}
			b := make([]byte, n)
			copy(b, buf[:n])
			s.SendControl(b)
		}
	}()
}
