// Package proxy 提供一个原生 scrcpy 协议代理：Go 中控保持设备唯一客户端，
// 原生 scrcpy 客户端通过本代理连接，Go 把设备的原始视频/音频/控制字节
// 同时分流给 web(WS) 和原生客户端，实现两边同时控制同一台设备。
package proxy

import (
	"log"
	"net"

	"scrcpy-lan/webui/internal/device"
)

// Server 监听一个端口，接受原生 scrcpy 客户端的三路连接
// （video/audio/control 顺序），桥接到设备会话。
type Server struct {
	addr string
	ln   net.Listener
	mgr  *device.Manager
}

func New(addr string, mgr *device.Manager) *Server {
	return &Server{addr: addr, mgr: mgr}
}

func (s *Server) Start() error {
	ln, err := net.Listen("tcp", s.addr)
	if err != nil {
		return err
	}
	s.ln = ln
	log.Printf("native proxy listening on %s (scrcpy --no-adb --tunnel-host <PC> --tunnel-port %s)", s.addr, portOf(s.addr))
	go s.acceptLoop()
	return nil
}

func portOf(addr string) string {
	if _, p, err := net.SplitHostPort(addr); err == nil {
		return p
	}
	return addr
}

func (s *Server) acceptLoop() {
	for {
		sess := s.mgr.First()
		if sess == nil {
			// 无设备会话，短暂等下一次轮询；为避免原生客户端连接堆积，
			// 先 accept 并关闭一次。
			if c, err := s.ln.Accept(); err != nil {
				return
			} else {
				c.Close()
			}
			continue
		}
		video, err := s.ln.Accept()
		if err != nil {
			return
		}
		// 原生客户端 connect video 后立即读 1B dummy，必须先发，否则它不会继续连 audio/control。
		video.Write([]byte{0x00})
		audio, err := s.ln.Accept()
		if err != nil {
			video.Close()
			return
		}
		control, err := s.ln.Accept()
		if err != nil {
			video.Close()
			audio.Close()
			return
		}
		log.Printf("native client attached to device %s", sess.Name())
		sess.AttachNative(video, audio, control)
	}
}
