package control

import (
	"bytes"
	"encoding/binary"
	"testing"
)

func TestInjectTouch(t *testing.T) {
	w := &Writer{}
	got := w.InjectTouch(Touch{
		Action: 0, PointerID: 1,
		X: 100, Y: 200, ScreenW: 1080, ScreenH: 2400,
		Pressure: 0xFFFF, ActionButton: 0, Buttons: 1,
	})
	want := make([]byte, 32)
	want[0] = TypeInjectTouchEvent
	want[1] = ActionDown
	binary.BigEndian.PutUint64(want[2:], 1)
	binary.BigEndian.PutUint32(want[10:], 100)
	binary.BigEndian.PutUint32(want[14:], 200)
	binary.BigEndian.PutUint16(want[18:], 1080)
	binary.BigEndian.PutUint16(want[20:], 2400)
	binary.BigEndian.PutUint16(want[22:], 0xFFFF)
	binary.BigEndian.PutUint32(want[24:], 0)
	binary.BigEndian.PutUint32(want[28:], 1)
	if !bytes.Equal(got, want) {
		t.Fatalf("touch = %x, want %x", got, want)
	}
}

func TestInjectKey(t *testing.T) {
	w := &Writer{}
	got := w.InjectKey(Key{Action: KeyActionDown, Keycode: 4, Repeat: 0, MetaState: 0}) // BACK
	want := []byte{TypeInjectKeycode, KeyActionDown, 0, 0, 0, 4, 0, 0, 0, 0, 0, 0, 0, 0}
	if !bytes.Equal(got, want) {
		t.Fatalf("key = %x, want %x", got, want)
	}
}

func TestInjectText(t *testing.T) {
	w := &Writer{}
	got := w.InjectText("hi")
	want := append([]byte{TypeInjectText, 0, 0, 0, 2}, 'h', 'i')
	if !bytes.Equal(got, want) {
		t.Fatalf("text = %x, want %x", got, want)
	}
}

func TestEmptyMessages(t *testing.T) {
	w := &Writer{}
	if got := w.RotateDevice(); !bytes.Equal(got, []byte{TypeRotateDevice}) {
		t.Fatalf("rotate = %x", got)
	}
	if got := w.BackOrScreenOn(1); !bytes.Equal(got, []byte{TypeBackOrScreenOn, 1}) {
		t.Fatalf("back = %x", got)
	}
}
