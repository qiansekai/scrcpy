package device

import (
	"encoding/binary"
	"fmt"
	"io"
)

// AudioFrame is one Opus/AAC/FLAC packet as produced by the device.
type AudioFrame struct {
	Pts    uint64
	Config bool
	Data   []byte
}

// AudioStream parses the device's audio socket: 4-byte codec id, then the
// same [12B frame meta + payload]* framing as video (no dummy byte, no
// device name, no session meta).
type AudioStream struct {
	r     io.Reader
	Codec [4]byte
	hdr   [12]byte
	frame []byte
}

func NewAudioStream(r io.Reader) *AudioStream {
	return &AudioStream{r: r}
}

// ReadMeta consumes the 4-byte audio codec id ("opus", "aac", "flac", "raw").
func (a *AudioStream) ReadMeta() error {
	if _, err := io.ReadFull(a.r, a.Codec[:]); err != nil {
		return fmt.Errorf("read audio codec id: %w", err)
	}
	return nil
}

// Next reads the next audio packet.
func (a *AudioStream) Next() (*AudioFrame, error) {
	if _, err := io.ReadFull(a.r, a.hdr[:]); err != nil {
		return nil, err
	}
	ptsAndFlags := binary.BigEndian.Uint64(a.hdr[0:8])
	size := int(binary.BigEndian.Uint32(a.hdr[8:12]))
	if size < 0 || size > 1<<20 {
		return nil, fmt.Errorf("invalid audio frame size %d", size)
	}
	if cap(a.frame) < size {
		a.frame = make([]byte, size)
	}
	buf := a.frame[:size]
	if _, err := io.ReadFull(a.r, buf); err != nil {
		return nil, err
	}
	return &AudioFrame{
		Pts:    ptsAndFlags & ptsMask,
		Config: ptsAndFlags&packetFlagConfig != 0,
		Data:   buf,
	}, nil
}
