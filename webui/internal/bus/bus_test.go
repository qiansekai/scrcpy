package bus

import (
	"bytes"
	"encoding/binary"
	"math"
	"sync"
	"testing"

	"scrcpy-lan/webui/internal/control"
)

// mathRound 是测试用取整助手，与 bus 包内 round 语义一致（四舍五入 + int32 截断）。
func mathRound(f float64) int32 { return int32(math.Round(f)) }

// newConfiguredBus 构造一个装好主控/被控/分辨率的总线，便于各测试复用。
func newConfiguredBus(master DimensionSet, slaveDims map[string]Dimensions) *Bus {
	b := NewBus()
	if master.ok {
		b.SetMaster(master.id)
		b.RegisterDim(master.id, master.dim)
	}
	ids := make([]string, 0, len(slaveDims))
	for id := range slaveDims {
		ids = append(ids, id)
	}
	b.SetSlaves(ids...)
	for id, d := range slaveDims {
		b.RegisterDim(id, d)
	}
	return b
}

// DimensionSet 携带主控 id/尺寸以及尺寸是否已知（ok=false 表示缺 dims）。
type DimensionSet struct {
	id  string
	dim Dimensions
	ok  bool
}

func TestForwardTouchScaling(t *testing.T) {
	tests := []struct {
		name      string
		master    DimensionSet
		masterMsg Msg // X/Y 为主控像素坐标，ScreenW/H 为主控尺寸（换算后应被替换）
		slave     Dimensions
		wantX     int32
		wantY     int32
		wantW     uint16
		wantH     uint16
		wantP     uint16
	}{
		{
			name:   "1080x2400 -> 720x1600 middle",
			master: DimensionSet{id: "m", dim: Dimensions{1080, 2400}, ok: true},
			masterMsg: Msg{
				Type: "touch", X: 540, Y: 1200,
				ScreenW: 1080, ScreenH: 2400, Action: control.ActionMove,
			},
			slave: Dimensions{720, 1600},
			wantX: int32(mathRound(540 * 720.0 / 1080)),
			wantY: int32(mathRound(1200 * 1600.0 / 2400)),
			wantW: 720, wantH: 1600, wantP: 0xFFFF,
		},
		{
			name:   "1080x2400 -> 1080x1920 center",
			master: DimensionSet{id: "m", dim: Dimensions{1080, 2400}, ok: true},
			masterMsg: Msg{
				Type: "touch", X: 540, Y: 1200,
				ScreenW: 1080, ScreenH: 2400, Action: control.ActionDown,
			},
			slave: Dimensions{1080, 1920},
			wantX: 540, wantY: int32(mathRound(1200 * 1920.0 / 2400)),
			wantW: 1080, wantH: 1920, wantP: 0xFFFF,
		},
		{
			name:   "round-half-up",
			master: DimensionSet{id: "m", dim: Dimensions{1080, 2400}, ok: true},
			masterMsg: Msg{
				Type: "touch", X: 100, Y: 50,
				ScreenW: 1080, ScreenH: 2400, Action: control.ActionUp,
			},
			slave: Dimensions{720, 1600},
			// 100*720/1080 = 66.666 -> 67；50*1600/2400 = 33.333 -> 33
			wantX: 67, wantY: 33,
			wantW: 720, wantH: 1600, wantP: 0xFFFF,
		},
		{
			name:   "origin-zero",
			master: DimensionSet{id: "m", dim: Dimensions{1080, 2400}, ok: true},
			masterMsg: Msg{
				Type: "touch", X: 0, Y: 0,
				ScreenW: 1080, ScreenH: 2400, Action: control.ActionDown,
			},
			slave: Dimensions{720, 1600},
			wantX: 0, wantY: 0,
			wantW: 720, wantH: 1600, wantP: 0xFFFF,
		},
		{
			name:   "max-coords-clamp-to-int32",
			master: DimensionSet{id: "m", dim: Dimensions{1080, 2400}, ok: true},
			masterMsg: Msg{
				Type: "touch", X: 1 << 62, Y: 1 << 62,
				ScreenW: 1080, ScreenH: 2400, Action: control.ActionMove,
			},
			slave: Dimensions{65535, 60000},
			wantX: math.MaxInt32, wantY: math.MaxInt32,
			wantW: 65535, wantH: 60000, wantP: 0xFFFF,
		},
		{
			name:   "pressure-preserved",
			master: DimensionSet{id: "m", dim: Dimensions{1080, 2400}, ok: true},
			masterMsg: Msg{
				Type: "touch", X: 1080, Y: 2400,
				ScreenW: 1080, ScreenH: 2400, Action: control.ActionMove, Pressure: 1234,
			},
			slave: Dimensions{1080, 2400},
			wantX: 1080, wantY: 2400,
			wantW: 1080, wantH: 2400, wantP: 1234,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			b := newConfiguredBus(tt.master, map[string]Dimensions{"s": tt.slave})
			got := b.Forward(tt.master.id, tt.masterMsg)
			if len(got) != 1 {
				t.Fatalf("Forward returned %d entries, want 1", len(got))
			}
			raw, ok := got["s"]
			if !ok {
				t.Fatalf("Forward missing slave id %q, got %v", "s", mapKeys(got))
			}
			if len(raw) != 32 {
				t.Fatalf("encoded touch length = %d, want 32", len(raw))
			}
			if raw[0] != control.TypeInjectTouchEvent {
				t.Fatalf("type byte = %d, want %d", raw[0], control.TypeInjectTouchEvent)
			}
			if raw[1] != byte(tt.masterMsg.Action) {
				t.Fatalf("action byte = %d, want %d", raw[1], tt.masterMsg.Action)
			}
			// PointerID 恒 0。
			if pid := binary.BigEndian.Uint64(raw[2:]); pid != 0 {
				t.Fatalf("pointer id = %d, want 0", pid)
			}
			if x := int32(binary.BigEndian.Uint32(raw[10:])); x != tt.wantX {
				t.Fatalf("x = %d, want %d", x, tt.wantX)
			}
			if y := int32(binary.BigEndian.Uint32(raw[14:])); y != tt.wantY {
				t.Fatalf("y = %d, want %d", y, tt.wantY)
			}
			if w := binary.BigEndian.Uint16(raw[18:]); w != tt.wantW {
				t.Fatalf("screenW = %d, want %d", w, tt.wantW)
			}
			if h := binary.BigEndian.Uint16(raw[20:]); h != tt.wantH {
				t.Fatalf("screenH = %d, want %d", h, tt.wantH)
			}
			if p := binary.BigEndian.Uint16(raw[22:]); p != tt.wantP {
				t.Fatalf("pressure = %d, want %d", p, tt.wantP)
			}
		})
	}
}

// TestForwardTouchMultipleSlaves 验证一次 Forward 能对不同分辨率的多个被控
// 分别算出各自的换算坐标。
func TestForwardTouchMultipleSlaves(t *testing.T) {
	master := Dimensions{1080, 2400}
	b := NewBus()
	b.SetMaster("m")
	b.RegisterDim("m", master)
	b.SetSlaves("a", "b", "c")
	b.RegisterDim("a", Dimensions{720, 1600})
	b.RegisterDim("b", Dimensions{1080, 1920})
	b.RegisterDim("c", Dimensions{1080, 2400})

	got := b.Forward("m", Msg{
		Type: "touch", X: 540, Y: 1200,
		ScreenW: 1080, ScreenH: 2400, Action: control.ActionMove,
	})
	if len(got) != 3 {
		t.Fatalf("got %d entries, want 3", len(got))
	}
	wants := map[string][2]int32{
		"a": {int32(mathRound(540 * 720.0 / 1080)), int32(mathRound(1200 * 1600.0 / 2400))},
		"b": {540, int32(mathRound(1200 * 1920.0 / 2400))},
		"c": {540, 1200},
	}
	for id, wc := range wants {
		raw, ok := got[id]
		if !ok {
			t.Fatalf("missing slave %q", id)
		}
		x := int32(binary.BigEndian.Uint32(raw[10:]))
		y := int32(binary.BigEndian.Uint32(raw[14:]))
		if x != wc[0] || y != wc[1] {
			t.Fatalf("slave %q coords = (%d,%d), want (%d,%d)", id, x, y, wc[0], wc[1])
		}
	}
}

// TestForwardNonTouchPassThrough 验证非 touch 消息对每个被控的编码与直接
// control.Writer 的编码逐字节一致。
func TestForwardNonTouchPassThrough(t *testing.T) {
	cw := &control.Writer{}
	tests := []struct {
		name string
		msg  Msg
		want []byte
	}{
		{"key", Msg{Type: "key", Action: control.KeyActionDown, Keycode: 4},
			cw.InjectKey(control.Key{Action: control.KeyActionDown, Keycode: 4})},
		{"text", Msg{Type: "text", Text: "hi"},
			cw.InjectText("hi")},
		{"rotate", Msg{Type: "rotate"},
			cw.RotateDevice()},
		{"audioDup-on", Msg{Type: "audioDup", On: true},
			cw.AudioDup(true)},
		{"audioDup-off", Msg{Type: "audioDup", On: false},
			cw.AudioDup(false)},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			b := NewBus()
			b.SetMaster("m")
			b.SetSlaves("s1", "s2")
			// 非 touch 消息不换算，但被控仍需有 dims 才编码。
			b.RegisterDim("s1", Dimensions{720, 1600})
			b.RegisterDim("s2", Dimensions{1080, 1920})

			got := b.Forward("m", tt.msg)
			if len(got) != 2 {
				t.Fatalf("got %d entries, want 2", len(got))
			}
			if !bytes.Equal(got["s1"], tt.want) {
				t.Fatalf("s1 = %x, want %x", got["s1"], tt.want)
			}
			if !bytes.Equal(got["s2"], tt.want) {
				t.Fatalf("s2 = %x, want %x", got["s2"], tt.want)
			}
		})
	}
}

// TestForwardUnsupportedTypes 验证多消息拼接/回执类消息返回空 map。
func TestForwardUnsupportedTypes(t *testing.T) {
	b := NewBus()
	b.SetMaster("m")
	b.RegisterDim("m", Dimensions{1080, 2400})
	b.SetSlaves("s")
	b.RegisterDim("s", Dimensions{720, 1600})

	for _, typ := range []string{"home", "back", "recents", "power", "getClipboard", "setClipboard", "unknown"} {
		got := b.Forward("m", Msg{Type: typ})
		if len(got) != 0 {
			t.Fatalf("type %q: got %d entries, want 0", typ, len(got))
		}
	}
}

// TestForwardMissingDims 验证主控或被控缺 dims 时跳过该被控。
func TestForwardMissingDims(t *testing.T) {
	t.Run("master missing dims", func(t *testing.T) {
		b := NewBus()
		b.SetMaster("m") // 不注册主控 dims
		b.SetSlaves("s")
		b.RegisterDim("s", Dimensions{720, 1600})

		got := b.Forward("m", Msg{Type: "touch", X: 10, Y: 20, ScreenW: 1080, ScreenH: 2400})
		if len(got) != 0 {
			t.Fatalf("got %d entries, want 0", len(got))
		}
	})

	t.Run("slave missing dims", func(t *testing.T) {
		b := NewBus()
		b.SetMaster("m")
		b.RegisterDim("m", Dimensions{1080, 2400})
		b.SetSlaves("withdim", "nodim")
		b.RegisterDim("withdim", Dimensions{720, 1600})
		// nodim 无 dims

		got := b.Forward("m", Msg{Type: "touch", X: 540, Y: 1200, ScreenW: 1080, ScreenH: 2400})
		if len(got) != 1 {
			t.Fatalf("got %d entries, want 1", len(got))
		}
		if _, ok := got["withdim"]; !ok {
			t.Fatalf("missing withdim entry")
		}
		if _, ok := got["nodim"]; ok {
			t.Fatalf("nodim should be skipped")
		}
	})

	t.Run("zero dimensions skipped", func(t *testing.T) {
		b := NewBus()
		b.SetMaster("m")
		b.RegisterDim("m", Dimensions{1080, 2400})
		b.SetSlaves("zero")
		b.RegisterDim("zero", Dimensions{0, 1600})

		got := b.Forward("m", Msg{Type: "touch", X: 540, Y: 1200, ScreenW: 1080, ScreenH: 2400})
		if len(got) != 0 {
			t.Fatalf("got %d entries, want 0", len(got))
		}
	})
}

// TestMasterExcludedFromSlaves 验证 SetSlaves 自动剔除主控本身。
func TestMasterExcludedFromSlaves(t *testing.T) {
	b := NewBus()
	b.SetMaster("m")
	b.SetSlaves("m", "a", "b")

	slaves := b.Slaves()
	if len(slaves) != 2 {
		t.Fatalf("slaves = %v, want [a b]", slaves)
	}
	if slaves[0] != "a" || slaves[1] != "b" {
		t.Fatalf("slaves = %v, want [a b]", slaves)
	}

	// 主控被剔除后，Forward 不会把主控自己当被控编码。
	b.RegisterDim("m", Dimensions{1080, 2400})
	b.RegisterDim("a", Dimensions{720, 1600})
	b.RegisterDim("b", Dimensions{1080, 1920})
	got := b.Forward("m", Msg{Type: "rotate"})
	if len(got) != 2 {
		t.Fatalf("got %d entries, want 2", len(got))
	}
	if _, ok := got["m"]; ok {
		t.Fatalf("master should not appear in result")
	}
}

// TestSetMasterEmptyCancels 验证空串取消主控。
func TestSetMasterEmptyCancels(t *testing.T) {
	b := NewBus()
	b.SetMaster("m")
	b.SetMaster("") // 取消
	b.SetSlaves("x")
	if got := b.Slaves(); len(got) != 1 || got[0] != "x" {
		t.Fatalf("slaves = %v, want [x]", got)
	}
}

// TestSetSlavesReplacesAndDedups 验证整体替换语义与去重。
func TestSetSlavesReplacesAndDedups(t *testing.T) {
	b := NewBus()
	b.SetSlaves("a", "b", "c")
	b.SetSlaves("x", "x", "y", "") // 去重 + 空串剔除
	got := b.Slaves()
	if len(got) != 2 || got[0] != "x" || got[1] != "y" {
		t.Fatalf("slaves = %v, want [x y]", got)
	}
}

// TestRegisterDimOverwriteAndUnregister 验证 dims 幂等覆盖与移除。
func TestRegisterDimOverwriteAndUnregister(t *testing.T) {
	b := NewBus()
	b.SetMaster("m")
	b.RegisterDim("m", Dimensions{1080, 2400})
	b.SetSlaves("s")
	b.RegisterDim("s", Dimensions{720, 1600})
	b.RegisterDim("s", Dimensions{1080, 1920}) // 覆盖

	got := b.Forward("m", Msg{Type: "touch", X: 540, Y: 1200, ScreenW: 1080, ScreenH: 2400})
	raw := got["s"]
	if w := binary.BigEndian.Uint16(raw[18:]); w != 1080 {
		t.Fatalf("screenW = %d, want 1080 (overwrite 生效)", w)
	}
	if h := binary.BigEndian.Uint16(raw[20:]); h != 1920 {
		t.Fatalf("screenH = %d, want 1920 (overwrite 生效)", h)
	}

	b.UnregisterDim("s")
	if got2 := b.Forward("m", Msg{Type: "rotate"}); len(got2) != 0 {
		t.Fatalf("after UnregisterDim, got %d entries, want 0", len(got2))
	}
}

// TestForwardCompletelyUninitialized 验证空总线（无主控/无被控）Forward 不 panic。
func TestForwardCompletelyUninitialized(t *testing.T) {
	b := NewBus()
	if got := b.Forward("ghost", Msg{Type: "touch", X: 1, Y: 2}); len(got) != 0 {
		t.Fatalf("got %d entries, want 0", len(got))
	}
	if got := b.Forward("ghost", Msg{Type: "key", Keycode: 3}); len(got) != 0 {
		t.Fatalf("got %d entries, want 0", len(got))
	}
}

// TestConcurrentAccess 在并发读写下跑 SetMaster/SetSlaves/RegisterDim/Forward，
// 配合 `go test -race` 检测数据竞争。
func TestConcurrentAccess(t *testing.T) {
	b := NewBus()
	b.SetMaster("m")
	b.RegisterDim("m", Dimensions{1080, 2400})
	b.SetSlaves("a", "b", "c")
	b.RegisterDim("a", Dimensions{720, 1600})
	b.RegisterDim("b", Dimensions{1080, 1920})

	var wg sync.WaitGroup
	// 写者：反复替换被控集合 / 改写分辨率。
	for i := 0; i < 4; i++ {
		wg.Add(1)
		go func(n int) {
			defer wg.Done()
			for j := 0; j < 1000; j++ {
				b.SetSlaves("a", "b", "c", "m") // 含主控，会被剔除
				b.RegisterDim("a", Dimensions{720 + (j % 10), 1600})
				b.UnregisterDim("a")
				b.RegisterDim("c", Dimensions{1080, 2400})
				b.SetMaster("m")
				b.SetMaster("")
				b.SetMaster("m")
			}
		}(i)
	}
	// 读者：Forward + Slaves。
	for i := 0; i < 4; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < 1000; j++ {
				b.Forward("m", Msg{Type: "touch", X: 540, Y: 1200, ScreenW: 1080, ScreenH: 2400})
				b.Forward("m", Msg{Type: "key", Keycode: 3})
				_ = b.Slaves()
			}
		}()
	}
	wg.Wait()
}

func mapKeys(m map[string][]byte) []string {
	ks := make([]string, 0, len(m))
	for k := range m {
		ks = append(ks, k)
	}
	return ks
}
