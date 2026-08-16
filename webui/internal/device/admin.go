package device

import (
	"bufio"
	"encoding/binary"
	"errors"
	"io"
	"net"
	"strconv"
	"strings"
	"time"
)

const defaultAdminPort = 27184

const (
	adminTypeShell  = 0x10
	adminTypeStream = 0x20
	adminTypeResult = 0x21
)

type ExecResult struct {
	ExitCode int
	Stdout   string
	Stderr   string
}

// ExecCommand runs one shell command on the device via its admin channel (27184).
// It dials per command for now; M-series work may move this to a long-lived session.
func (m *Manager) ExecCommand(id string, cmd string) (ExecResult, error) {
	m.mu.RLock()
	sess, ok := m.sessions[id]
	m.mu.RUnlock()
	if !ok {
		return ExecResult{}, errors.New("no such device session")
	}

	addr := net.JoinHostPort(sess.cfg.IP, strconv.Itoa(defaultAdminPort))
	if sess.cfg.Addr != "" {
		addr = net.JoinHostPort(sess.cfg.Addr, strconv.Itoa(defaultAdminPort))
	}

	conn, err := net.DialTimeout("tcp", addr, 3*time.Second)
	if err != nil {
		return ExecResult{}, err
	}
	defer conn.Close()

	data := []byte(cmd)
	req := make([]byte, 1+4+len(data))
	req[0] = adminTypeShell
	binary.BigEndian.PutUint32(req[1:], uint32(len(data)))
	copy(req[5:], data)
	if _, err := conn.Write(req); err != nil {
		return ExecResult{}, err
	}

	r := bufio.NewReader(conn)
	var stdout, stderr strings.Builder
	for {
		typ, err := r.ReadByte()
		if err != nil {
			return ExecResult{}, err
		}
		var lenBuf [4]byte
		if _, err := io.ReadFull(r, lenBuf[:]); err != nil {
			return ExecResult{}, err
		}
		n := binary.BigEndian.Uint32(lenBuf[:])
		payload := make([]byte, n)
		if _, err := io.ReadFull(r, payload); err != nil {
			return ExecResult{}, err
		}
		switch typ {
		case adminTypeStream:
			// 设备端不区分 stdout/stderr 的实时块，统一累积。
			stdout.Write(payload)
		case adminTypeResult:
			if len(payload) < 4 {
				return ExecResult{}, errors.New("short RESULT payload")
			}
			exit := int(int32(binary.BigEndian.Uint32(payload[0:4])))
			return ExecResult{ExitCode: exit, Stdout: stdout.String(), Stderr: stderr.String()}, nil
		}
	}
}
