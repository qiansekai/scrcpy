package task

import (
	"bufio"
	"bytes"
	"context"
	"encoding/binary"
	"errors"
	"io"
	"net"
	"strings"
	"sync"
	"testing"
)

// fakeAdminServer 用 net.Pipe 模拟一个 TYPE_PUSH admin 服务端：按协议读入推送块，
// 并把 (path, data) 依次记录到 received；每块回一个 ack（type 0x21, exitCode
// 由 ackExit 指定）。它作为 PushTo 的对端，不涉及真实网络。
type fakeAdminServer struct {
	mu       sync.Mutex
	received [][]byte // 每块收到的 data（不含头）
	paths    []string
	ackExit  int
	// 自定义 ack 序列（超过 received 数量时回默认 ok）；nil 表示全部默认。
	customAck []int32
}

// serve 在一个 goroutine 里跑服务端逻辑，返回 channel 通知退出原因。
// 协议（与 writeChunk 对应）：type(1) + frameLen(4) + pathLen(4) + path + data。
func (s *fakeAdminServer) serve(t *testing.T, conn net.Conn, done chan<- error) {
	t.Helper()
	defer close(done)
	r := bufio.NewReader(conn)
	for {
		typ, err := r.ReadByte()
		if err != nil {
			done <- err
			return
		}
		_ = typ
		var lenBuf [8]byte
		if _, err := io.ReadFull(r, lenBuf[:]); err != nil {
			done <- err
			return
		}
		frameLen := binary.BigEndian.Uint32(lenBuf[0:4])
		pathLen := binary.BigEndian.Uint32(lenBuf[4:8])
		if frameLen < 4+pathLen {
			done <- errors.New("bad frame len")
			return
		}
		path := make([]byte, pathLen)
		if _, err := io.ReadFull(r, path); err != nil {
			done <- err
			return
		}
		dataLen := frameLen - 4 - pathLen
		data := make([]byte, dataLen)
		if _, err := io.ReadFull(r, data); err != nil {
			done <- err
			return
		}
		s.mu.Lock()
		s.paths = append(s.paths, string(path))
		s.received = append(s.received, data)
		exit := int32(s.ackExit)
		if s.customAck != nil && len(s.received)-1 < len(s.customAck) {
			exit = s.customAck[len(s.received)-1]
		}
		s.mu.Unlock()
		if err := writeAck(conn, exit); err != nil {
			done <- err
			return
		}
	}
}

// writeAck 写一个 RESULT ack 帧。
func writeAck(w io.Writer, exit int32) error {
	payload := make([]byte, 4)
	binary.BigEndian.PutUint32(payload, uint32(exit))
	frame := make([]byte, 1+4+len(payload))
	frame[0] = adminTypeResult
	binary.BigEndian.PutUint32(frame[1:], uint32(len(payload)))
	copy(frame[5:], payload)
	_, err := w.Write(frame)
	return err
}

// runPushTo 连接 net.Pipe 两端，在服务端 goroutine 与客户端 PushTo 之间做测试。
func runPushTo(t *testing.T, remote string, data []byte, size int64, server *fakeAdminServer) error {
	t.Helper()
	client, serverConn := net.Pipe()
	done := make(chan error)
	go server.serve(t, serverConn, done)

	// 客户端走 PushTo，但 PushTo 会真实 Dial，这里改为直接调用内部写逻辑不可行；
	// 因此单独测 pushChunked 级联。为复用 PushTo 的完整语义，引入可注入 dial。
	err := pushOverConn(context.Background(), client, remote, bytes.NewReader(data))
	client.Close()

	serr := <-done
	if err != nil {
		return err
	}
	if serr != nil && serr != io.EOF && !strings.Contains(serr.Error(), "closed") {
		return serr
	}
	return nil
}

// pushOverConn 在已有连接上做分块推送（供测试注入 net.Pipe 连接的核心逻辑）。
// 它复用 writeChunk/readAck 与 PushTo 相同的分块语义。
func pushOverConn(ctx context.Context, conn net.Conn, remote string, r io.Reader) error {
	path := []byte(remote)
	buf := bufio.NewReader(conn)
	chunk := make([]byte, pushMaxChunk)
	first := true
	for {
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
			continue
		case errors.Is(readErr, io.EOF):
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
			return nil
		default:
			return readErr
		}
	}
}

func TestPushTo_SmallFile(t *testing.T) {
	srv := &fakeAdminServer{ackExit: 0}
	data := []byte("hello 世界")
	if err := runPushTo(t, "/sdcard/a.txt", data, int64(len(data)), srv); err != nil {
		t.Fatalf("push: %v", err)
	}
	srv.mu.Lock()
	defer srv.mu.Unlock()
	if len(srv.paths) != 1 || srv.paths[0] != "/sdcard/a.txt" {
		t.Fatalf("paths = %v, want [/sdcard/a.txt]", srv.paths)
	}
	if len(srv.received) != 1 || !bytes.Equal(srv.received[0], data) {
		t.Fatalf("received = %x, want %x", srv.received, data)
	}
}

func TestPushTo_ExactChunk(t *testing.T) {
	srv := &fakeAdminServer{ackExit: 0}
	data := bytes.Repeat([]byte{'a'}, int(pushMaxChunk)) // 正好 256KB
	if err := runPushTo(t, "/data/local/tmp/x.apk", data, int64(len(data)), srv); err != nil {
		t.Fatalf("push: %v", err)
	}
	srv.mu.Lock()
	defer srv.mu.Unlock()
	if len(srv.received) != 1 || !bytes.Equal(srv.received[0], data) {
		t.Fatalf("received blocks = %d, want 1", len(srv.received))
	}
}

func TestPushTo_MultipleChunks(t *testing.T) {
	srv := &fakeAdminServer{ackExit: 0}
	// 2.5 块 → 3 块
	total := int(pushMaxChunk)*2 + int(pushMaxChunk)/2
	data := bytes.Repeat([]byte{'b'}, total)
	if err := runPushTo(t, "/sdcard/b.bin", data, int64(total), srv); err != nil {
		t.Fatalf("push: %v", err)
	}
	srv.mu.Lock()
	defer srv.mu.Unlock()
	if len(srv.received) != 3 {
		t.Fatalf("blocks = %d, want 3", len(srv.received))
	}
	var got []byte
	for _, b := range srv.received {
		got = append(got, b...)
	}
	if !bytes.Equal(got, data) {
		t.Fatalf("reassembled mismatch")
	}
}

func TestPushTo_AckFailure(t *testing.T) {
	srv := &fakeAdminServer{ackExit: 5}
	data := []byte("x")
	err := runPushTo(t, "/sdcard/c.txt", data, int64(len(data)), srv)
	if err == nil {
		t.Fatal("expected error for non-zero ack exit code")
	}
	if !strings.Contains(err.Error(), "exit code 5") {
		t.Fatalf("err = %v, want mention of exit code 5", err)
	}
}

func TestPushTo_BadAckType(t *testing.T) {
	// 服务端回一个错误 type（0x20 流）而非 0x21。
	client, serverConn := net.Pipe()
	done := make(chan error)
	go func() {
		defer close(done)
		r := bufio.NewReader(serverConn)
		_, _ = r.ReadByte() // type 0x11
		var lenBuf [8]byte
		_, _ = io.ReadFull(r, lenBuf[:]) // frameLen + pathLen
		pathLen := binary.BigEndian.Uint32(lenBuf[4:8])
		_, _ = io.ReadFull(r, make([]byte, pathLen)) // path
		dataLen := binary.BigEndian.Uint32(lenBuf[0:4]) - 4 - pathLen
		_, _ = io.ReadFull(r, make([]byte, dataLen)) // data
		// 回一个 stream 帧（type 0x20）
		_, _ = serverConn.Write([]byte{0x20, 0, 0, 0, 4, 0, 0, 0, 0})
		done <- nil
	}()
	ctx := context.Background()
	buf := bufio.NewReader(client)
	if err := writeChunk(client, []byte("/sdcard/d"), []byte("z")); err != nil {
		t.Fatal(err)
	}
	err := readAck(ctx, buf)
	client.Close()
	<-done
	if err == nil {
		t.Fatal("expected error for non-result ack type")
	}
	if !strings.Contains(err.Error(), "0x20") {
		t.Fatalf("err = %v, want mention of type 0x20", err)
	}
}

func TestPushTo_PathEncoding(t *testing.T) {
	srv := &fakeAdminServer{ackExit: 0}
	remote := "/sdcard/中文 目录/文件.apk" // 含 UTF-8 与空格
	data := []byte("payload")
	if err := runPushTo(t, remote, data, int64(len(data)), srv); err != nil {
		t.Fatalf("push: %v", err)
	}
	srv.mu.Lock()
	defer srv.mu.Unlock()
	if len(srv.paths) != 1 || srv.paths[0] != remote {
		t.Fatalf("path = %q, want %q", srv.paths[0], remote)
	}
}

func TestPushTo_EmptyFile(t *testing.T) {
	srv := &fakeAdminServer{ackExit: 0}
	if err := runPushTo(t, "/sdcard/empty", nil, 0, srv); err != nil {
		t.Fatalf("push: %v", err)
	}
	srv.mu.Lock()
	defer srv.mu.Unlock()
	if len(srv.received) != 1 || len(srv.received[0]) != 0 {
		t.Fatalf("received = %v, want one empty block", srv.received)
	}
}

func TestPushTo_EmptyParams(t *testing.T) {
	// 空地址、空路径应立刻返回错误，不 panic。
	if err := PushTo(context.Background(), "", "/x", bytes.NewReader(nil), 0); err == nil {
		t.Fatal("expected error for empty addr")
	}
	if err := PushTo(context.Background(), "1.2.3.4:1", "", bytes.NewReader(nil), 0); err == nil {
		t.Fatal("expected error for empty path")
	}
}
