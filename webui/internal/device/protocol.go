package device

import (
	"encoding/binary"
	"fmt"
	"io"
	"strings"
)

const (
	DeviceNameLen = 64

	packetFlagSession  uint64 = 1 << 63
	packetFlagConfig   uint64 = 1 << 62
	packetFlagKeyFrame uint64 = 1 << 61
	ptsMask            uint64 = (1 << 61) - 1
)

var CodecH264 = [4]byte{'h', '2', '6', '4'}

type SessionInfo struct {
	Width  int
	Height int
}

type VideoFrame struct {
	Pts      uint64
	Config   bool
	KeyFrame bool
	Data     []byte
}

type VideoStream struct {
	r       io.Reader
	Device  string
	Codec   [4]byte
	Session SessionInfo
	hdr     [12]byte
	frame   []byte
}

func NewVideoStream(r io.Reader) *VideoStream {
	return &VideoStream{r: r}
}

// ReadMeta consumes the dummy byte, 64-byte device name, and 4-byte codec id.
func (vs *VideoStream) ReadMeta() error {
	var dummy [1]byte
	if _, err := io.ReadFull(vs.r, dummy[:]); err != nil {
		return fmt.Errorf("read dummy byte: %w", err)
	}
	var name [DeviceNameLen]byte
	if _, err := io.ReadFull(vs.r, name[:]); err != nil {
		return fmt.Errorf("read device name: %w", err)
	}
	vs.Device = strings.TrimRight(string(name[:]), "\x00")
	if _, err := io.ReadFull(vs.r, vs.Codec[:]); err != nil {
		return fmt.Errorf("read codec id: %w", err)
	}
	if vs.Codec != CodecH264 {
		return fmt.Errorf("unsupported video codec %q, only h264", vs.Codec)
	}
	return nil
}

// Next returns the next video frame, or a non-nil *SessionInfo once when the
// device reports its display size at stream start.
func (vs *VideoStream) Next() (*VideoFrame, *SessionInfo, error) {
	if _, err := io.ReadFull(vs.r, vs.hdr[:]); err != nil {
		return nil, nil, err
	}
	if binary.BigEndian.Uint32(vs.hdr[0:4])&0x80000000 != 0 {
		vs.Session = SessionInfo{
			Width:  int(binary.BigEndian.Uint32(vs.hdr[4:8])),
			Height: int(binary.BigEndian.Uint32(vs.hdr[8:12])),
		}
		return nil, &vs.Session, nil
	}
	ptsAndFlags := binary.BigEndian.Uint64(vs.hdr[0:8])
	size := int(binary.BigEndian.Uint32(vs.hdr[8:12]))
	if size < 0 || size > 16<<20 {
		return nil, nil, fmt.Errorf("invalid frame size %d", size)
	}
	if cap(vs.frame) < size {
		vs.frame = make([]byte, size)
	}
	buf := vs.frame[:size]
	if _, err := io.ReadFull(vs.r, buf); err != nil {
		return nil, nil, err
	}
	return &VideoFrame{
		Pts:      ptsAndFlags & ptsMask,
		Config:   ptsAndFlags&packetFlagConfig != 0,
		KeyFrame: ptsAndFlags&packetFlagKeyFrame != 0,
		Data:     buf,
	}, nil, nil
}
