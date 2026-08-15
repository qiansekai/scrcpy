package control

import (
	"encoding/binary"
	"errors"
	"fmt"
	"io"
)

const (
	DevMsgClipboard    = 0
	DevMsgAckClipboard = 1
	DevMsgUhidOutput   = 2
)

type DeviceMessage struct {
	Type     int
	Text     string
	Sequence uint64
	ID       uint16
	Data     []byte
}

type Reader struct{ r io.Reader }

func NewReader(r io.Reader) *Reader { return &Reader{r: r} }

func (rd *Reader) Read() (*DeviceMessage, error) {
	var hdr [1]byte
	if _, err := io.ReadFull(rd.r, hdr[:]); err != nil {
		return nil, err
	}
	msg := &DeviceMessage{Type: int(hdr[0])}
	switch msg.Type {
	case DevMsgClipboard:
		var lenBuf [4]byte
		if _, err := io.ReadFull(rd.r, lenBuf[:]); err != nil {
			return nil, err
		}
		n := int(binary.BigEndian.Uint32(lenBuf[:]))
		if n < 0 || n > 256*1024 {
			return nil, errors.New("invalid clipboard length")
		}
		data := make([]byte, n)
		if _, err := io.ReadFull(rd.r, data); err != nil {
			return nil, err
		}
		msg.Text = string(data)
	case DevMsgAckClipboard:
		var seq [8]byte
		if _, err := io.ReadFull(rd.r, seq[:]); err != nil {
			return nil, err
		}
		msg.Sequence = binary.BigEndian.Uint64(seq[:])
	case DevMsgUhidOutput:
		var h [4]byte
		if _, err := io.ReadFull(rd.r, h[:]); err != nil {
			return nil, err
		}
		msg.ID = binary.BigEndian.Uint16(h[0:2])
		n := int(binary.BigEndian.Uint16(h[2:4]))
		data := make([]byte, n)
		if _, err := io.ReadFull(rd.r, data); err != nil {
			return nil, err
		}
		msg.Data = data
	default:
		return nil, fmt.Errorf("unknown device message type %d", msg.Type)
	}
	return msg, nil
}
