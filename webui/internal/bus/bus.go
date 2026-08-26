// Package bus 实现主控-被控（master-slave）控制总线：把主控设备发来的控制
// 消息按分辨率换算后广播给多台被控设备。
//
// 本包只负责「算坐标 + 编码」，不直接对外发送消息；编码结果交给集成层
// 通过各自的 SendControl 发往被控，便于单元测试。
//
// 为避免 import cycle，本包不 import ws 包，Msg 结构体为 ws.CtrlMsg 的
// 字段级复制（并额外带 Pressure 字段）。
package bus

import (
	"math"
	"sync"

	"scrcpy-lan/webui/internal/control"
)

// Dimensions 表示一台设备的屏幕分辨率（像素）。
type Dimensions struct{ W, H int }

// Msg 与 ws.CtrlMsg 同构（字段级复制，禁止 import ws 包避免 cycle）。
// X/Y 为主控设备的像素坐标（仅 touch 消息有效）。
type Msg struct {
	Type      string  `json:"type"`
	X         float64 `json:"x,omitempty"`
	Y         float64 `json:"y,omitempty"`
	ScreenW   int     `json:"screenW,omitempty"`
	ScreenH   int     `json:"screenH,omitempty"`
	Action    int     `json:"action,omitempty"`
	Keycode   int     `json:"keycode,omitempty"`
	Text      string  `json:"text,omitempty"`
	Clipboard string  `json:"clipboard,omitempty"`
	CopyKey   int     `json:"copyKey,omitempty"`
	Paste     bool    `json:"paste,omitempty"`
	On        bool    `json:"on,omitempty"`
	Pressure  uint16  `json:"pressure,omitempty"`
}

// Bus 维护主控/"被控集合/各设备分辨率，并提供 Forward 做坐标换算与编码。
// 并发安全：内部用 RWMutex 保护全部状态。
type Bus struct {
	mu     sync.RWMutex
	master string
	dims   map[string]Dimensions
	// slaves 用 slice + set 双结构：slice 保证稳定顺序，set 用于 O(1) 剔除。
	slaves    []string
	slavesSet map[string]struct{}
}

// NewBus 返回一个空的总线实例。
func NewBus() *Bus {
	return &Bus{
		dims:      make(map[string]Dimensions),
		slavesSet: make(map[string]struct{}),
	}
}

// SetMaster 设置主控设备；空串表示取消主控（回到无主控状态）。
func (b *Bus) SetMaster(id string) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.master = id
}

// Master 返回当前主控设备 ID；无主控时返回空串。
func (b *Bus) Master() string {
	b.mu.RLock()
	defer b.mu.RUnlock()
	return b.master
}

// SetSlaves 整体替换被控集合；自动剔除主控本身与空 id。
func (b *Bus) SetSlaves(ids ...string) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.slaves = b.slaves[:0]
	for k := range b.slavesSet {
		delete(b.slavesSet, k)
	}
	for _, id := range ids {
		if id == "" {
			continue
		}
		if id == b.master {
			continue
		}
		if _, dup := b.slavesSet[id]; dup {
			continue
		}
		b.slavesSet[id] = struct{}{}
		b.slaves = append(b.slaves, id)
	}
}

// Slaves 返回当前被控设备列表（稳定顺序，不含主控）。
// 调用方拿到的是副本，随后对 Bus 的修改不会影响已返回的切片。
func (b *Bus) Slaves() []string {
	b.mu.RLock()
	defer b.mu.RUnlock()
	out := make([]string, len(b.slaves))
	copy(out, b.slaves)
	return out
}

// RegisterDim 幂等覆盖记录设备分辨率。session 建立时由集成层调用。
func (b *Bus) RegisterDim(id string, d Dimensions) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.dims[id] = d
}

// UnregisterDim 移除设备分辨率记录。会话结束时由集成层调用。
func (b *Bus) UnregisterDim(id string) {
	b.mu.Lock()
	defer b.mu.Unlock()
	delete(b.dims, id)
}

// Forward 把主控消息广播编码到所有被控，返回 map[被控ID][]byte。
//
// 语义：
//   - m 来自主控。仅当 m.Type == "touch" 时对每个被控做坐标换算：
//     x' = round(x * slaveW / masterW)，y' = round(y * slaveH / masterH)，
//     并把 ScreenW/ScreenH 换成被控尺寸；换算结果仍是 control.Touch 编码
//     （PointerID 恒 0；Pressure 为 0 时用 0xFFFF）。
//   - 主控或被控缺 dims 时，该被控不出现在返回 map。
//   - 非 touch 消息对每个（有 dims 的）被控原样编码一份，支持单条编码类：
//     key→InjectKey、text→InjectText、rotate→RotateDevice、audioDup→AudioDup(On)。
//
// 注意：home/back/recents 是 DOWN+UP 双消息拼接、power 仅 DOWN、
// getClipboard/setClipboard 依赖回执顺序，这些「非单条编码」类消息本方法
// 不处理而返回空 map，由集成层决定是否转发（参见 ws 包 handler.go 的
// route 实现）。
func (b *Bus) Forward(master string, m Msg) map[string][]byte {
	b.mu.RLock()
	masterDim, masterOK := b.dims[master]
	slaves := make([]string, len(b.slaves))
	copy(slaves, b.slaves)
	dims := make(map[string]Dimensions, len(slaves))
	for _, id := range slaves {
		if d, ok := b.dims[id]; ok {
			dims[id] = d
		}
	}
	b.mu.RUnlock()

	// 只处理「单条编码」类消息；其余返回空 map，由集成层决定。
	switch m.Type {
	case "touch", "key", "text", "rotate", "audioDup":
	default:
		return nil
	}

	// touch 消息依赖主控分辨率换算坐标；主控缺 dims 时无被控可编码。
	if m.Type == "touch" && !masterOK {
		return nil
	}

	out := make(map[string][]byte)
	cw := &control.Writer{}
	for _, id := range slaves {
		slaveDim, ok := dims[id]
		if !ok {
			continue
		}
		switch m.Type {
		case "touch":
			c, err := encodeTouch(m, masterDim, slaveDim)
			if err != nil {
				continue
			}
			out[id] = c
		case "key":
			out[id] = cw.InjectKey(control.Key{Action: m.Action, Keycode: int32(m.Keycode)})
		case "text":
			out[id] = cw.InjectText(m.Text)
		case "rotate":
			out[id] = cw.RotateDevice()
		case "audioDup":
			out[id] = cw.AudioDup(m.On)
		}
	}
	return out
}

// encodeTouch 按 master/slave 分辨率把主控坐标换算成被控坐标并编码。
// 任一尺寸非正时返回错误，由调用方跳过该被控。
func encodeTouch(m Msg, masterDim, slaveDim Dimensions) ([]byte, error) {
	if masterDim.W <= 0 || masterDim.H <= 0 || slaveDim.W <= 0 || slaveDim.H <= 0 {
		return nil, errInvalidDims
	}
	x := round(m.X * float64(slaveDim.W) / float64(masterDim.W))
	y := round(m.Y * float64(slaveDim.H) / float64(masterDim.H))
	pressure := m.Pressure
	if pressure == 0 {
		pressure = 0xFFFF
	}
	return (&control.Writer{}).InjectTouch(control.Touch{
		Action:    m.Action,
		PointerID: 0,
		X:         x,
		Y:         y,
		ScreenW:   uint16(slaveDim.W),
		ScreenH:   uint16(slaveDim.H),
		Pressure:  pressure,
	}), nil
}

// round 对 f 做四舍五入取整（round(x * slaveW / masterW)），并截断到 int32
// 以匹配 control.Touch.X/Y 的 int32 类型，避免维度极大时溢出。
func round(f float64) int32 {
	if f >= math.MaxInt32 {
		return math.MaxInt32
	}
	if f <= math.MinInt32 {
		return math.MinInt32
	}
	return int32(math.Round(f))
}

// errInvalidDims 表示主控或被控分辨率非法（宽或高非正）。
var errInvalidDims = errValue{}

type errValue struct{}

func (errValue) Error() string { return "invalid dimensions" }
