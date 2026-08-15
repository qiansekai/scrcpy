package control

import (
	"bytes"
	"encoding/binary"
	"io"
	"testing"
)

func TestReadClipboard(t *testing.T) {
	var buf bytes.Buffer
	buf.WriteByte(DevMsgClipboard)
	binary.Write(&buf, binary.BigEndian, uint32(4))
	buf.WriteString("test")
	rd := NewReader(&buf)
	m, err := rd.Read()
	if err != nil {
		t.Fatalf("Read: %v", err)
	}
	if m.Type != DevMsgClipboard || m.Text != "test" {
		t.Fatalf("clipboard = %+v", m)
	}
}

func TestReadAckClipboard(t *testing.T) {
	var buf bytes.Buffer
	buf.WriteByte(DevMsgAckClipboard)
	binary.Write(&buf, binary.BigEndian, uint64(42))
	rd := NewReader(&buf)
	m, err := rd.Read()
	if err != nil {
		t.Fatalf("Read: %v", err)
	}
	if m.Sequence != 42 {
		t.Fatalf("ack = %+v", m)
	}
}

func TestReadUhidOutput(t *testing.T) {
	var buf bytes.Buffer
	buf.WriteByte(DevMsgUhidOutput)
	binary.Write(&buf, binary.BigEndian, uint16(7))
	binary.Write(&buf, binary.BigEndian, uint16(2))
	buf.Write([]byte{0xAA, 0xBB})
	rd := NewReader(&buf)
	m, err := rd.Read()
	if err != nil {
		t.Fatalf("Read: %v", err)
	}
	if m.ID != 7 || len(m.Data) != 2 {
		t.Fatalf("uhid = %+v", m)
	}
}

func TestReadEOF(t *testing.T) {
	rd := NewReader(bytes.NewReader(nil))
	if _, err := rd.Read(); err != io.EOF {
		t.Fatalf("expected EOF, got %v", err)
	}
}
