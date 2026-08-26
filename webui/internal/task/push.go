package task

import (
	"bufio"
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"net"
	"time"
)

// admin 协议常量（与 device/admin.go 保持一致）。
// TODO(集成者)：设备端 AdminServer 需实现 TYPE_PUSH（0x11），见
// docs/superpowers/specs/2026-08-27-webui-full-stack.md 2.1 节；同时为
// device.Manager 添加 AdminAddr(id string) (string, error)。在设备端补齐前，
// 推送会因 AdminAddr 不可用或 TYPE_PUSH 不支持而返回错误。
const (
	adminTypePush         = 0x11
	adminTypeResult       = 0x21
	pushMaxChunk    int64 = 256 * 1024 // 单块最大 256KB
)

// errAdminAddrUnavailable 表示设备 Manager 未提供 admin 地址（未实现可选接口）。
var errAdminAddrUnavailable = errors.New("admin address unavailable")

// adminDialer 是本包定义的包内接口，用于从 Manager 获取 admin 地址。
// 类型断言 Manager 是否实现它；实现失败则该设备推送返回 error。
type adminDialer interface {
	AdminAddr(id string) (string, error)
}

// PushTo 把 r 的内容（共 size 字节）经 admin 协议 TYPE_PUSH 推送到 adminAddr
// 对应的设备，落地文件为 remote。协议：每次新建 TCP 连接，循环分块（≤256KB）：
// 写请求帧 1B 0x11 + 4B frameLen + [4B pathLen + path(UTF8) + data]；每块后读
// 一个响应帧（1B type + 4B len + payload），type 必须 0x21 且 payload 首 4 字节
// exitCode == 0，否则报错；写完关闭连接。首块即截断创建、后续追加（设备端
// 语义，Go 侧只是顺序发送）。size 仅用于边界判断，不受 r 实际字节数约束，
// 传入 -1 表示未知大小（仍按读取到的内容分块发送）。
func PushTo(ctx context.Context, adminAddr, remote string, r io.Reader, size int64) error {
	if adminAddr == "" {
		return errors.New("empty admin address")
	}
	if remote == "" {
		return errors.New("empty remote path")
	}
	if r == nil {
		// 空 reader 也允许：发一个空文件（首块 path 即截断创建）。
		r = emptyReader{}
	}

	conn, err := dialCtx(ctx, adminAddr)
	if err != nil {
		return err
	}
	defer conn.Close()

	path := []byte(remote)
	buf := bufio.NewReader(conn)
	chunk := make([]byte, pushMaxChunk)

	first := true
	for {
		if err := ctx.Err(); err != nil {
			return err
		}
		n, readErr := io.ReadFull(r, chunk)
		if n > 0 {
			if err := writeChunk(conn, path, chunk[:n]); err != nil {
				return err
			}
			if err := readAck(ctx, buf); err != nil {
				return err
			}
			first = false
		}
		switch {
		case readErr == nil:
			// 整块读满，继续下一块。
			continue
		case errors.Is(readErr, io.EOF):
			// 读到末尾。若一块都没发（空文件/空 reader），补发一个空块
			// 以完成「首块截断创建」语义。
			if first {
				if err := writeChunk(conn, path, nil); err != nil {
					return err
				}
				if err := readAck(ctx, buf); err != nil {
					return err
				}
			}
			return nil
		case errors.Is(readErr, io.ErrUnexpectedEOF):
			// 最后一块不足 256KB，已由上面的 writeChunk 发送。
			return nil
		default:
			return readErr
		}
	}
}

// writeChunk 写一个推送请求帧：1B type(0x11) + 4B frameLen + payload，
// payload = 4B pathLen + path + data。frameLen = 4 + len(path) + len(data)，
// 与 admin 帧格式（1B type + 4B len + payload）一致，服务端据此按块切分。
func writeChunk(w io.Writer, path, data []byte) error {
	pathLen := uint32(len(path))
	frameLen := 4 + pathLen + uint32(len(data))
	req := make([]byte, 1+4+int(frameLen))
	req[0] = adminTypePush
	binary.BigEndian.PutUint32(req[1:], frameLen)
	binary.BigEndian.PutUint32(req[5:], pathLen)
	copy(req[9:], path)
	copy(req[9+len(path):], data)
	_, err := w.Write(req)
	return err
}

// readAck 读一个 ack 帧并校验：type 必须 0x21、payload ≥4B、exitCode == 0。
func readAck(ctx context.Context, r io.Reader) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	var typBuf [1]byte
	if _, err := io.ReadFull(r, typBuf[:]); err != nil {
		return err
	}
	typ := typBuf[0]
	var lenBuf [4]byte
	if _, err := io.ReadFull(r, lenBuf[:]); err != nil {
		return err
	}
	n := binary.BigEndian.Uint32(lenBuf[:])
	if n < 4 {
		return fmt.Errorf("short ack payload: %d bytes", n)
	}
	payload := make([]byte, n)
	if _, err := io.ReadFull(r, payload); err != nil {
		return err
	}
	if typ != adminTypeResult {
		return fmt.Errorf("unexpected ack type 0x%x, want 0x21", typ)
	}
	exit := int(int32(binary.BigEndian.Uint32(payload[0:4])))
	if exit != 0 {
		return fmt.Errorf("device rejected push: exit code %d", exit)
	}
	return nil
}

// dialCtx 建立到 adminAddr 的 TCP 连接，ctx 取消可中断。
func dialCtx(ctx context.Context, addr string) (net.Conn, error) {
	var d net.Dialer
	conn, err := d.DialContext(ctx, "tcp", addr)
	if err != nil {
		return nil, err
	}
	// 增加写超时，防止对端不读 ack 导致死等。
	_ = conn.SetDeadline(time.Now().Add(30 * time.Second))
	return conn, nil
}

// emptyReader 永远返回 EOF 的空读取器。
type emptyReader struct{}

func (emptyReader) Read(p []byte) (int, error) { return 0, io.EOF }
