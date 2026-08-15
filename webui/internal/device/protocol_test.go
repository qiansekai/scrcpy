package device

import (
	"bytes"
	"encoding/binary"
	"testing"
)

// buildStream 构造一个合法的视频流字节序列。
func buildStream(t *testing.T) []byte {
	t.Helper()
	var buf bytes.Buffer
	buf.WriteByte(0x00) // dummy
	name := make([]byte, DeviceNameLen)
	copy(name, "Pixel 7")
	buf.Write(name)         // 64B device name
	buf.Write(CodecH264[:]) // codec id "h264"

	// session meta: flags=0x80000000, width=1080, height=2400
	writeU32(&buf, 0x80000000)
	writeU32(&buf, 1080)
	writeU32(&buf, 2400)

	// frame 1: config (SPS/PPS)，PTS=0
	writeFrame(&buf, 0x4000000000000000, []byte{0x00, 0x00, 0x00, 0x01, 0x67})
	// frame 2: keyframe PTS=1000
	writeFrame(&buf, 1000|0x2000000000000000, []byte{0x00, 0x00, 0x00, 0x01, 0x65})
	// frame 3: normal PTS=2000
	writeFrame(&buf, 2000, []byte{0x00, 0x00, 0x00, 0x01, 0x61})
	return buf.Bytes()
}

func writeU32(b *bytes.Buffer, v uint32) {
	var tmp [4]byte
	binary.BigEndian.PutUint32(tmp[:], v)
	b.Write(tmp[:])
}

func writeFrame(b *bytes.Buffer, ptsAndFlags uint64, payload []byte) {
	var tmp [8]byte
	binary.BigEndian.PutUint64(tmp[:], ptsAndFlags)
	b.Write(tmp[:])
	writeU32(b, uint32(len(payload)))
	b.Write(payload)
}

func TestVideoStreamParse(t *testing.T) {
	vs := NewVideoStream(bytes.NewReader(buildStream(t)))
	if err := vs.ReadMeta(); err != nil {
		t.Fatalf("ReadMeta: %v", err)
	}
	if vs.Device != "Pixel 7" {
		t.Errorf("device = %q, want %q", vs.Device, "Pixel 7")
	}
	if vs.Codec != CodecH264 {
		t.Errorf("codec = %q, want h264", vs.Codec)
	}

	// 第一包是 session
	f, sess, err := vs.Next()
	if err != nil {
		t.Fatalf("Next session: %v", err)
	}
	if f != nil || sess == nil || sess.Width != 1080 || sess.Height != 2400 {
		t.Fatalf("session = %+v, want 1080x2400", sess)
	}

	// 第二包 config 帧
	f, sess, err = vs.Next()
	if err != nil {
		t.Fatalf("Next config: %v", err)
	}
	if sess != nil || !f.Config || f.KeyFrame || f.Pts != 0 {
		t.Fatalf("config frame = %+v", f)
	}

	// 第三包 keyframe，PTS=1000
	f, _, err = vs.Next()
	if err != nil {
		t.Fatalf("Next key: %v", err)
	}
	if f.Pts != 1000 {
		t.Errorf("keyframe pts = %d, want 1000", f.Pts)
	}

	// 第四包普通帧，PTS=2000
	f, _, err = vs.Next()
	if err != nil {
		t.Fatalf("Next frame: %v", err)
	}
	if f.Config || f.KeyFrame || f.Pts != 2000 {
		t.Fatalf("frame = %+v", f)
	}

	// 流结束
	if _, _, err = vs.Next(); err == nil {
		t.Fatal("expected EOF")
	}
}

func TestVideoStreamRejectsNonH264(t *testing.T) {
	var buf bytes.Buffer
	buf.WriteByte(0x00)
	buf.Write(make([]byte, DeviceNameLen))
	buf.Write([]byte{'h', '2', '6', '5'}) // h265
	vs := NewVideoStream(&buf)
	if err := vs.ReadMeta(); err == nil {
		t.Fatal("expected error for non-h264 codec")
	}
}
