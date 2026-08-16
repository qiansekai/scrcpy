package control

import "encoding/binary"

const (
	TypeInjectKeycode      = 0
	TypeInjectText         = 1
	TypeInjectTouchEvent   = 2
	TypeBackOrScreenOn     = 4
	TypeExpandNotification = 5
	TypeExpandSettings     = 6
	TypeCollapsePanels     = 7
	TypeGetClipboard       = 8
	TypeSetClipboard       = 9
	TypeRotateDevice       = 11
)

const (
	ActionDown = 0
	ActionUp   = 1
	ActionMove = 2
)

const (
	KeyActionDown = 0
	KeyActionUp   = 1
)

type Touch struct {
	Action           int
	PointerID        uint64
	X, Y             int32
	ScreenW, ScreenH uint16
	Pressure         uint16
	ActionButton     int32
	Buttons          int32
}

type Key struct {
	Action    int
	Keycode   int32
	Repeat    int32
	MetaState int32
}

type Writer struct{}

// InjectTouch serializes TYPE_INJECT_TOUCH_EVENT (32 bytes, big-endian).
func (w *Writer) InjectTouch(t Touch) []byte {
	b := make([]byte, 32)
	b[0] = TypeInjectTouchEvent
	b[1] = byte(t.Action)
	binary.BigEndian.PutUint64(b[2:], t.PointerID)
	binary.BigEndian.PutUint32(b[10:], uint32(t.X))
	binary.BigEndian.PutUint32(b[14:], uint32(t.Y))
	binary.BigEndian.PutUint16(b[18:], t.ScreenW)
	binary.BigEndian.PutUint16(b[20:], t.ScreenH)
	binary.BigEndian.PutUint16(b[22:], t.Pressure)
	binary.BigEndian.PutUint32(b[24:], uint32(t.ActionButton))
	binary.BigEndian.PutUint32(b[28:], uint32(t.Buttons))
	return b
}

// InjectKey serializes TYPE_INJECT_KEYCODE (14 bytes, big-endian).
func (w *Writer) InjectKey(k Key) []byte {
	b := make([]byte, 14)
	b[0] = TypeInjectKeycode
	b[1] = byte(k.Action)
	binary.BigEndian.PutUint32(b[2:], uint32(k.Keycode))
	binary.BigEndian.PutUint32(b[6:], uint32(k.Repeat))
	binary.BigEndian.PutUint32(b[10:], uint32(k.MetaState))
	return b
}

func (w *Writer) InjectText(s string) []byte {
	data := []byte(s)
	b := make([]byte, 1+4+len(data))
	b[0] = TypeInjectText
	binary.BigEndian.PutUint32(b[1:], uint32(len(data)))
	copy(b[5:], data)
	return b
}

func (w *Writer) BackOrScreenOn(action byte) []byte {
	return []byte{TypeBackOrScreenOn, action}
}
func (w *Writer) ExpandNotificationPanel() []byte { return []byte{TypeExpandNotification} }
func (w *Writer) ExpandSettingsPanel() []byte     { return []byte{TypeExpandSettings} }
func (w *Writer) CollapsePanels() []byte          { return []byte{TypeCollapsePanels} }
func (w *Writer) RotateDevice() []byte            { return []byte{TypeRotateDevice} }

// GetClipboard serializes TYPE_GET_CLIPBOARD. copyKey: 0=none, 1=COPY, 2=CUT.
func (w *Writer) GetClipboard(copyKey byte) []byte {
	return []byte{TypeGetClipboard, copyKey}
}

// SetClipboard serializes TYPE_SET_CLIPBOARD. sequence=0 asks the device not to
// acknowledge; paste=true makes the device press PASTE after setting.
func (w *Writer) SetClipboard(text string, paste bool, sequence uint64) []byte {
	data := []byte(text)
	b := make([]byte, 1+8+1+4+len(data))
	b[0] = TypeSetClipboard
	binary.BigEndian.PutUint64(b[1:], sequence)
	if paste {
		b[9] = 1
	}
	binary.BigEndian.PutUint32(b[10:], uint32(len(data)))
	copy(b[14:], data)
	return b
}
