package device

import (
	"bufio"
	"encoding/binary"
	"errors"
	"io"
	"net"
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
	addr, err := m.AdminAddr(id)
	if err != nil {
		return ExecResult{}, err
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
			// RESULT payload = exitCode(4) + stdoutLen(4) + stdout + stderrLen(4) + stderr。
			if len(payload) < 4 {
				return ExecResult{}, errors.New("short RESULT payload")
			}
			res := ExecResult{
				ExitCode: int(int32(binary.BigEndian.Uint32(payload[0:4]))),
				Stdout:   stdout.String(),
				Stderr:   stderr.String(),
			}
			if len(payload) >= 12 {
				soLen := int(binary.BigEndian.Uint32(payload[4:8]))
				if 8+soLen <= len(payload) {
					res.Stdout += string(payload[8 : 8+soLen])
				}
				if 8+soLen+4 <= len(payload) {
					seLen := int(binary.BigEndian.Uint32(payload[8+soLen : 12+soLen]))
					if 12+soLen+seLen <= len(payload) {
						res.Stderr += string(payload[12+soLen : 12+soLen+seLen])
					}
				}
			}
			return res, nil
		}
	}
}
