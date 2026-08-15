package device

import (
	"io"
	"net"
	"strconv"
	"sync"
	"time"

	"scrcpy-lan/webui/internal/control"
)

const defaultVideoPort = 27183

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
}

func NewStreamSession(cfg SessionConfig, b Broadcaster) *StreamSession {
	if cfg.VideoPort == 0 {
		cfg.VideoPort = defaultVideoPort
	}
	return &StreamSession{
		cfg:  cfg,
		b:    b,
		ctrl: make(chan []byte, 256),
		stop: make(chan struct{}),
		done: make(chan struct{}),
	}
}

func (s *StreamSession) Start() { go s.run() }

func (s *StreamSession) Stop() {
	close(s.stop)
	<-s.done
}

func (s *StreamSession) Name() string {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.name
}

// SendControl queues a control message to be written to the device. Blocks if
// the queue is full so no touch-up is ever dropped.
func (s *StreamSession) SendControl(b []byte) {
	select {
	case s.ctrl <- b:
	case <-s.stop:
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
		if backoff < 30*time.Second {
			backoff *= 2
		}
	}
}

func (s *StreamSession) connectOnce() error {
	addr := net.JoinHostPort(s.cfg.IP, strconv.Itoa(s.cfg.VideoPort))
	if s.cfg.Addr != "" {
		addr = s.cfg.Addr
	}

	video, err := net.Dial("tcp", addr)
	if err != nil {
		return err
	}
	defer video.Close()
	audio, err := net.Dial("tcp", addr)
	if err != nil {
		return err
	}
	defer audio.Close()
	ctl, err := net.Dial("tcp", addr)
	if err != nil {
		return err
	}
	defer ctl.Close()

	vs := NewVideoStream(video)
	if err := vs.ReadMeta(); err != nil {
		return err
	}
	s.mu.Lock()
	s.name = vs.Device
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
		as := NewAudioStream(audio)
		if err := as.ReadMeta(); err != nil {
			fail(err)
			return
		}
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
				s.b.PublishSession(s.cfg.ID, *sess)
				continue
			}
			s.b.PublishFrame(s.cfg.ID, f)
		}
	}()

	// device->client reader.
	go func() {
		cr := control.NewReader(ctl)
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
