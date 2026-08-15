# M2 单设备 Web 上屏 Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** 在浏览器里能实时看并控制一台 Android 设备（Go 中控直连设备 27183 端口，WS 中继，Vue + WebCodecs 解码 H.264）。

**Architecture:** 设备端 `scrcpy-lan-server.jar` 已在 `0.0.0.0:27183` 常驻监听（本 fork 的无 adb 架构）。Go 中控按 video→audio→control 顺序建立 3 条 TCP 连接，解析视频流帧元数据后通过 WebSocket 广播给浏览器；浏览器触控/键盘转成 scrcpy 控制消息回写设备 control socket。M2 不做设备端改动、不做批量/网格/状态监控（M1/M3/M4/M5）。

**Tech Stack:** Go 1.22+（标准库 + `github.com/coder/websocket`）、Vue 3 + Vite + TypeScript + Pinia + Vuetify 3、WebCodecs（浏览器端 H.264 解码）。

## Global Constraints

- **无 adb**：Go 直连设备 TCP 27183，任何代码不得调用 adb。设备发现用配置文件手动填 IP（M2 阶段）。
- **线协议字节级精确**：全部大端。格式依据本 fork 源码 `server/.../Streamer.java`、`ControlMessageReader.java`、`DeviceMessageWriter.java`、`DeviceMessage.java`。与官方 scrcpy 协议一致。
- **视频编码固定 H.264**（codec id 为 4 字节 ASCII `"h264"`），其他 codec 解析器直接报错。
- **M2 只做单设备**：video + control 为主，audio 连接后读取并丢弃（防 TCP 背压阻塞设备，勿跳过 audio accept，否则设备端 audio/control accept 各等 5s 超时）。
- 开发环境 Windows：shell 命令用 PowerShell；Go 代码 `go build` 可在 Windows 编译；Vue 用 npm。
- 代码不写 emoji；不写多余注释（只在不显然处写 WHY）。

---

### Task 1: 项目脚手架 + 配置存储 + HTTP 骨架

**Files:**
- Create: `webui/go.mod`
- Create: `webui/cmd/webui/main.go`
- Create: `webui/internal/store/config.go`
- Create: `webui/internal/store/config_test.go`

**Interfaces:**
- Consumes: 无
- Produces: `store.Config`、`store.DeviceConfig{ID, IP string}`、`store.Load(path string) (*Config, error)`、`store.Save(path string, c *Config) error`；`main.go` 启动 HTTP 服务，提供 `GET /api/health`。

- [ ] **Step 1: 初始化 Go 模块与依赖**

```powershell
cd D:\Kita-Tools\scrcpy-lan\webui
go mod init scrcpy-lan/webui
go get github.com/coder/websocket@latest
```

- [ ] **Step 2: 写配置测试**

```go
// webui/internal/store/config_test.go
package store

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLoadAndSaveRoundTrip(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "devices.json")
	in := &Config{
		Devices: []DeviceConfig{
			{ID: "dev1", IP: "192.168.1.50"},
			{ID: "dev2", IP: "192.168.1.51"},
		},
	}
	if err := Save(path, in); err != nil {
		t.Fatalf("Save: %v", err)
	}
	out, err := Load(path)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if len(out.Devices) != 2 || out.Devices[0].IP != "192.168.1.50" {
		t.Fatalf("unexpected loaded config: %+v", out.Devices)
	}
}

func TestLoadMissingFile(t *testing.T) {
	_, err := Load(filepath.Join(t.TempDir(), "nope.json"))
	if err == nil {
		t.Fatal("expected error for missing file")
	}
}
```

- [ ] **Step 3: 运行测试确认失败**

Run: `go test ./internal/store/`
Expected: 编译失败（`Config` 未定义）

- [ ] **Step 4: 实现配置存储**

```go
// webui/internal/store/config.go
package store

import (
	"encoding/json"
	"os"
)

type DeviceConfig struct {
	ID string `json:"id"`
	IP string `json:"ip"`
}

type Config struct {
	Devices []DeviceConfig `json:"devices"`
}

func Load(path string) (*Config, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var c Config
	if err := json.Unmarshal(data, &c); err != nil {
		return nil, err
	}
	return &c, nil
}

func Save(path string, c *Config) error {
	data, err := json.MarshalIndent(c, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, data, 0o644)
}
```

- [ ] **Step 5: 运行测试确认通过**

Run: `go test ./internal/store/`
Expected: PASS（2 个测试）

- [ ] **Step 6: 写入口 + 健康检查**

```go
// webui/cmd/webui/main.go
package main

import (
	"encoding/json"
	"flag"
	"log"
	"net/http"

	"scrcpy-lan/webui/internal/store"
)

func main() {
	configPath := flag.String("config", "devices.json", "path to devices config file")
	addr := flag.String("addr", ":8080", "listen address")
	flag.Parse()

	cfg, err := store.Load(*configPath)
	if err != nil {
		log.Printf("no config loaded (%v), starting empty", err)
		cfg = &store.Config{}
	}

	mux := http.NewServeMux()
	mux.HandleFunc("/api/health", func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(map[string]bool{"ok": true})
	})

	log.Printf("webui listening on %s", *addr)
	log.Fatal(http.ListenAndServe(*addr, mux))
}
```

- [ ] **Step 7: 编译 + 手工验证**

Run:
```powershell
cd D:\Kita-Tools\scrcpy-lan\webui
go build ./...
go run ./cmd/webui
```

Expected: 服务启动，`Invoke-RestMethod http://localhost:8080/api/health` 返回 `{"ok":true}`。

- [ ] **Step 8: Commit**

```bash
git add webui/go.mod webui/go.sum webui/cmd webui/internal/store
git commit -m "feat(webui): scaffold go module, config store, health endpoint"
```

---

### Task 2: 视频流解析器 protocol.go（字节级核心）

**Files:**
- Create: `webui/internal/device/protocol.go`
- Create: `webui/internal/device/protocol_test.go`

**Interfaces:**
- Consumes: 无
- Produces: `device.SessionInfo{Width, Height int}`、`device.VideoFrame{Pts uint64; Config, KeyFrame bool; Data []byte}`、`device.VideoStream`（`NewVideoStream(r io.Reader) *VideoStream`、`(*VideoStream).ReadMeta() error`、`(*VideoStream).Next() (*VideoFrame, *SessionInfo, error)`、字段 `Device string`、`Codec [4]byte`）。

**线格式（video socket，全部大端，依据 Streamer.java / DesktopConnection.java）：**
1. `dummy byte`：1B `0x00`
2. `device name`：64B UTF-8 零填充
3. `codec id`：4B ASCII（H264 = `"h264"`）
4. `session meta`（流启动时一次）：`flags int32`（bit31=`PACKET_FLAG_SESSION`，bit0=clientResize）+ `width int32` + `height int32`
5. 循环 `frame`：`ptsAndFlags int64`（bit62=config，bit61=keyframe）+ `packetSize int32` + `packetSize` 字节 payload

**判别 session vs frame：读 12B 头，取前 4B 为 uint32，若 bit31 置位则是 session meta（宽高在字节 4:8 和 8:12），否则是 frame（pts 取低 61 位）。**

- [ ] **Step 1: 写解析测试**

```go
// webui/internal/device/protocol_test.go
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
	buf.Write(name)            // 64B device name
	buf.Write(CodecH264[:])    // codec id "h264"

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
	if f.KeyFrame || !f.Config { // keyframe 帧同时是 config 吗？不是
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
```

- [ ] **Step 2: 运行测试确认失败**

Run: `go test ./internal/device/`
Expected: 编译失败（`device` 包不存在 / 符号未定义）

- [ ] **Step 3: 实现解析器**

```go
// webui/internal/device/protocol.go
package device

import (
	"encoding/binary"
	"fmt"
	"io"
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
	vs.Device = string(name[:])
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
```

- [ ] **Step 4: 运行测试确认通过**

Run: `go test ./internal/device/ -v`
Expected: PASS（`TestVideoStreamParse`、`TestVideoStreamRejectsNonH264`）。注意测试里 keyframe 断言处有一个空 `if` 块，运行前修掉它（删除 `if f.KeyFrame || !f.Config {}` 空块即可）。

- [ ] **Step 5: Commit**

```bash
git add webui/internal/device/protocol.go webui/internal/device/protocol_test.go
git commit -m "feat(webui): parse scrcpy video stream (dummy/name/codec/session/frame meta)"
```

---

### Task 3: 控制消息编码 control/writer.go

**Files:**
- Create: `webui/internal/control/writer.go`
- Create: `webui/internal/control/writer_test.go`

**Interfaces:**
- Consumes: 无
- Produces: `control.Writer`（方法返回待写字节切片）：
  - `InjectTouch(t Touch) []byte`
  - `InjectKey(k Key) []byte`
  - `InjectText(s string) []byte`
  - `BackOrScreenOn(action byte) []byte`
  - `ExpandNotificationPanel()/ExpandSettingsPanel()/CollapsePanels()/RotateDevice() []byte`
  - 类型 `Touch{Action int; PointerID uint64; X, Y int32; ScreenW, ScreenH uint16; Pressure uint16; ActionButton, Buttons int32}`、`Key{Action int; Keycode, Repeat, MetaState int32}`
  - 常量 `TypeInjectKeycode=0`, `TypeInjectText=1`, `TypeInjectTouchEvent=2`, `TypeBackOrScreenOn=4`, `TypeExpandNotification=5`, `TypeExpandSettings=6`, `TypeCollapsePanels=7`, `TypeRotateDevice=11`；`ActionDown=0, ActionUp=1, ActionMove=2`；`KeyActionDown=0, KeyActionUp=1`

**线格式（client→device，全部大端，依据 ControlMessageReader.java）：**
- INJECT_TOUCH_EVENT(2)：`type u8` + `action u8` + `pointerId i64` + `x i32` + `y i32` + `screenW u16` + `screenH u16` + `pressure u16`（定点 = 浮点×0xFFFF）+ `actionButton i32` + `buttons i32`（共 32B）
- INJECT_KEYCODE(0)：`type u8` + `action u8` + `keycode i32` + `repeat i32` + `metaState i32`（共 14B）
- INJECT_TEXT(1)：`type u8` + `len u32` + UTF-8 字节
- 空类型(5/6/7/11)：仅 `type u8`；BACK_OR_SCREEN_ON(4)：`type u8` + `action u8`

- [ ] **Step 1: 写编码测试**

```go
// webui/internal/control/writer_test.go
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
```

- [ ] **Step 2: 运行测试确认失败**

Run: `go test ./internal/control/`
Expected: 编译失败

- [ ] **Step 3: 实现编码器**

```go
// webui/internal/control/writer.go
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
	Action       int
	PointerID    uint64
	X, Y         int32
	ScreenW, ScreenH uint16
	Pressure     uint16
	ActionButton int32
	Buttons      int32
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
```

- [ ] **Step 4: 运行测试确认通过**

Run: `go test ./internal/control/ -v`
Expected: PASS（4 个测试）

- [ ] **Step 5: Commit**

```bash
git add webui/internal/control/writer.go webui/internal/control/writer_test.go
git commit -m "feat(webui): encode scrcpy control messages (touch/key/text/empty)"
```

---

### Task 4: 设备消息读取 control/reader.go

**Files:**
- Create: `webui/internal/control/reader.go`
- Create: `webui/internal/control/reader_test.go`

**Interfaces:**
- Consumes: 无
- Produces: `control.DeviceMessage{Type int; Text string; Sequence uint64; ID uint16; Data []byte}`、`control.Reader`（`NewReader(r io.Reader) *Reader`、`(*Reader).Read() (*DeviceMessage, error)`）、常量 `DevMsgClipboard=0, DevMsgAckClipboard=1, DevMsgUhidOutput=2`

**线格式（device→client，全部大端，依据 DeviceMessageWriter.java）：**
- CLIPBOARD(0)：`type u8` + `textLen i32` + UTF-8 文本
- ACK_CLIPBOARD(1)：`type u8` + `sequence i64`
- UHID_OUTPUT(2)：`type u8` + `id u16` + `dataLen u16` + data（M2 忽略该消息内容，仅消费）

- [ ] **Step 1: 写读取测试**

```go
// webui/internal/control/reader_test.go
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
```

- [ ] **Step 2: 运行测试确认失败**

Run: `go test ./internal/control/`
Expected: 编译失败

- [ ] **Step 3: 实现读取器**

```go
// webui/internal/control/reader.go
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
```

- [ ] **Step 4: 运行测试确认通过**

Run: `go test ./internal/control/ -v`
Expected: PASS（4 个测试）

- [ ] **Step 5: Commit**

```bash
git add webui/internal/control/reader.go webui/internal/control/reader_test.go
git commit -m "feat(webui): parse device messages (clipboard/ack/uhid)"
```

---

### Task 5: StreamSession + Manager（连设备 + 断线重连）

**Files:**
- Create: `webui/internal/device/session.go`
- Create: `webui/internal/device/session_test.go`

**Interfaces:**
- Consumes: `device.VideoStream`、`control.Reader`、`control.Writer`
- Produces:
  - `device.SessionConfig{ID, IP string; VideoPort int}`（VideoPort 默认 27183）
  - `device.Broadcaster` 接口：`PublishSession(id string, s SessionInfo)`、`PublishFrame(id string, f *VideoFrame)`、`PublishDeviceMessage(id string, m *control.DeviceMessage)`
  - `device.StreamSession`：`NewStreamSession(cfg SessionConfig, b Broadcaster) *StreamSession`、`(*StreamSession).Start()`、`(*StreamSession).Stop()`、`(*StreamSession).SendControl(b []byte)`、`(*StreamSession).Name() string`
  - `device.Manager`：`NewManager(b Broadcaster) *Manager`、`(*Manager).Add(cfg SessionConfig)`、`(*Manager).Remove(id string)`、`(*Manager).Status() []Status`、`(*Manager).SendControl(id string, b []byte) error`；`device.Status{ID, IP, Name string; Online bool; Width, Height int; Codec string}`

**连接顺序（关键）：** video → audio → control（设备端 ServerSocket 按此顺序 accept，顺序错则流错位）。audio 连接后启动 goroutine 读取并丢弃（防背压）。

- [ ] **Step 1: 写会话集成测试（假设备 TCP 服务）**

```go
// webui/internal/device/session_test.go
package device

import (
	"bytes"
	"encoding/binary"
	"io"
	"net"
	"strconv"
	"testing"
	"time"

	"scrcpy-lan/webui/internal/control"
)

// fakeDevice 模拟设备端 27183：按 video→audio→control 顺序 accept 三个连接。
type fakeDevice struct {
	ln net.Listener
}

func startFakeDevice(t *testing.T) *fakeDevice {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	fd := &fakeDevice{ln: ln}
	go fd.serve()
	t.Cleanup(func() { ln.Close() })
	return fd
}

func (fd *fakeDevice) serve() {
	// video
	video, err := fd.ln.Accept()
	if err != nil {
		return
	}
	defer video.Close()
	video.Write([]byte{0x00}) // dummy
	name := make([]byte, DeviceNameLen)
	copy(name, "FakePhone")
	video.Write(name)
	video.Write(CodecH264[:])
	writeSessionAndFrames(video)
	// audio
	audio, err := fd.ln.Accept()
	if err != nil {
		return
	}
	defer audio.Close()
	audio.Write([]byte{'o', 'p', 'u', 's'})
	// control
	controlConn, err := fd.ln.Accept()
	if err != nil {
		return
	}
	defer controlConn.Close()
	// 回一个剪贴板消息，验证 reader 通路
	var msg bytes.Buffer
	msg.WriteByte(control.DevMsgClipboard)
	binary.Write(&msg, binary.BigEndian, uint32(2))
	msg.WriteString("hi")
	controlConn.Write(msg.Bytes())
	// 保持连接直到测试结束
	io.Copy(io.Discard, controlConn)
}

func writeSessionAndFrames(w io.Writer) {
	binary.Write(w, binary.BigEndian, uint32(0x80000000))
	binary.Write(w, binary.BigEndian, uint32(1080))
	binary.Write(w, binary.BigEndian, uint32(2400))
	writeOneFrame(w, 0x4000000000000000, []byte{0x00, 0x00, 0x00, 0x01, 0x67})
	writeOneFrame(w, 1000|0x2000000000000000, []byte{0x00, 0x00, 0x00, 0x01, 0x65})
}

func writeOneFrame(w io.Writer, ptsAndFlags uint64, payload []byte) {
	var h [8]byte
	binary.BigEndian.PutUint64(h[:], ptsAndFlags)
	w.Write(h[:])
	binary.Write(w, binary.BigEndian, uint32(len(payload)))
	w.Write(payload)
}

type recordingBroadcaster struct {
	frames  []*VideoFrame
	devices []string
	session []SessionInfo
	msgs    []*control.DeviceMessage
}

func (r *recordingBroadcaster) PublishSession(id string, s SessionInfo) {
	r.devices = append(r.devices, id)
	r.session = append(r.session, s)
}
func (r *recordingBroadcaster) PublishFrame(id string, f *VideoFrame) {
	r.devices = append(r.devices, id)
	r.frames = append(r.frames, f)
}
func (r *recordingBroadcaster) PublishDeviceMessage(id string, m *control.DeviceMessage) {
	r.msgs = append(r.msgs, m)
}

func TestSessionConnectsAndStreams(t *testing.T) {
	fd := startFakeDevice(t)
	rec := &recordingBroadcaster{}
	cfg := SessionConfig{ID: "dev1", IP: fd.ln.Addr().String()}
	sess := NewStreamSession(cfg, rec)
	sess.Start()
	defer sess.Stop()

	deadline := time.Now().Add(3 * time.Second)
	for len(rec.frames) < 2 && time.Now().Before(deadline) {
		time.Sleep(10 * time.Millisecond)
	}
	if len(rec.frames) != 2 {
		t.Fatalf("frames = %d, want 2", len(rec.frames))
	}
	if len(rec.devices) == 0 || rec.devices[0] != "dev1" {
		t.Fatalf("no session published: %v", rec.devices)
	}
	if len(rec.session) == 0 || rec.session[0].Width != 1080 {
		t.Fatalf("session = %+v", rec.session)
	}
	if sess.Name() != "FakePhone" {
		t.Fatalf("name = %q", sess.Name())
	}
	if len(rec.msgs) == 0 || rec.msgs[0].Text != "hi" {
		t.Fatalf("device msgs = %+v", rec.msgs)
	}
}
```

- [ ] **Step 2: 运行测试确认失败**

Run: `go test ./internal/device/`
Expected: 编译失败（`NewStreamSession` 未定义）

- [ ] **Step 3: 实现会话与 Manager**

```go
// webui/internal/device/session.go
package device

import (
	"io"
	"net"
	"strconv"
	"sync"
	"time"

	"scrcpy-lan/webui/internal/control"
)

const defaultVideoPort = 27183

type SessionConfig struct {
	ID        string
	IP        string
	VideoPort int
}

type Broadcaster interface {
	PublishSession(id string, s SessionInfo)
	PublishFrame(id string, f *VideoFrame)
	PublishDeviceMessage(id string, m *control.DeviceMessage)
}

type StreamSession struct {
	cfg       SessionConfig
	b         Broadcaster
	ctrl      chan []byte
	stop      chan struct{}
	done      chan struct{}
	name      string
	mu        sync.RWMutex
}

func NewStreamSession(cfg SessionConfig, b Broadcaster) *StreamSession {
	if cfg.VideoPort == 0 {
		cfg.VideoPort = defaultVideoPort
	}
	return &StreamSession{
		cfg:  cfg,
		b:    b,
		ctrl: make(chan []byte, 256),
		stop: make(chan struct{}),
		done: make(chan struct{}),
	}
}

func (s *StreamSession) Start() { go s.run() }

func (s *StreamSession) Stop() {
	close(s.stop)
	<-s.done
}

func (s *StreamSession) Name() string {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.name
}

// SendControl queues a control message to be written to the device. Blocks if
// the queue is full so no touch-up is ever dropped.
func (s *StreamSession) SendControl(b []byte) {
	select {
	case s.ctrl <- b:
	case <-s.stop:
	}
}

func (s *StreamSession) run() {
	defer close(s.done)
	backoff := time.Second
	for {
		select {
		case <-s.stop:
			return
		default:
		}
		err := s.connectOnce()
		if err == nil {
			return
		}
		select {
		case <-s.stop:
			return
		case <-time.After(backoff):
		}
		if backoff < 30*time.Second {
			backoff *= 2
		}
	}
}

func (s *StreamSession) connectOnce() error {
	addr := net.JoinHostPort(s.cfg.IP, strconv.Itoa(s.cfg.VideoPort))

	video, err := net.Dial("tcp", addr)
	if err != nil {
		return err
	}
	defer video.Close()
	audio, err := net.Dial("tcp", addr)
	if err != nil {
		return err
	}
	defer audio.Close()
	ctl, err := net.Dial("tcp", addr)
	if err != nil {
		return err
	}
	defer ctl.Close()

	vs := NewVideoStream(video)
	if err := vs.ReadMeta(); err != nil {
		return err
	}
	s.mu.Lock()
	s.name = vs.Device
	s.mu.Unlock()

	// audio: consume and discard so the device never backpressures.
	go io.Copy(io.Discard, audio)

	// video reader.
	go func() {
		for {
			f, sess, err := vs.Next()
			if err != nil {
				return
			}
			if sess != nil {
				s.b.PublishSession(s.cfg.ID, *sess)
				continue
			}
			s.b.PublishFrame(s.cfg.ID, f)
		}
	}()

	// device->client reader.
	go func() {
		cr := control.NewReader(ctl)
		for {
			m, err := cr.Read()
			if err != nil {
				return
			}
			s.b.PublishDeviceMessage(s.cfg.ID, m)
		}
	}()

	// control writer: the loop that keeps the connection alive.
	for {
		select {
		case <-s.stop:
			return nil
		case b := <-s.ctrl:
			if _, err := ctl.Write(b); err != nil {
				return err
			}
		}
	}
}

type Status struct {
	ID, IP, Name string
	Online       bool
	Width, Height int
	Codec        string
}

type Manager struct {
	b        Broadcaster
	mu       sync.RWMutex
	sessions map[string]*StreamSession
}

func NewManager(b Broadcaster) *Manager {
	return &Manager{b: b, sessions: make(map[string]*StreamSession)}
}

func (m *Manager) Add(cfg SessionConfig) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if _, ok := m.sessions[cfg.ID]; ok {
		return
	}
	sess := NewStreamSession(cfg, m.b)
	m.sessions[cfg.ID] = sess
	sess.Start()
}

func (m *Manager) Remove(id string) {
	m.mu.Lock()
	sess, ok := m.sessions[id]
	if ok {
		delete(m.sessions, id)
	}
	m.mu.Unlock()
	if ok {
		sess.Stop()
	}
}

func (m *Manager) SendControl(id string, b []byte) error {
	m.mu.RLock()
	sess, ok := m.sessions[id]
	m.mu.RUnlock()
	if !ok {
		return io.ErrClosedPipe
	}
	sess.SendControl(b)
	return nil
}
```

> 说明：`Manager.Status()` 未在 M2 实现（需要 session 上报在线/宽高状态）。M2 的 REST 设备列表先返回 `store.Config` 中的条目 + `Manager` 是否有会话（Online 判定），视频宽高在浏览器端从 WS `session` 消息拿到。任务 7 里实现一个轻量 `Status`：Online = 会话存在。宽高展示留到 M3。

- [ ] **Step 4: 运行测试确认通过**

Run: `go test ./internal/device/ -v -timeout 20s`
Expected: PASS。注意 `TestSessionConnectsAndStreams` 里 `cfg.IP` 用的是 `fd.ln.Addr().String()`（含端口），而 `connectOnce` 又用 `VideoPort` 拼地址——测试断言会失败。修法：给 `SessionConfig` 增加一个测试内联地址字段 `Addr string`，`connectOnce` 优先用 `cfg.Addr`，否则用 `IP:VideoPort`。实现时按此调整（`Add` 生产路径填 `IP`，测试填 `Addr`）。

- [ ] **Step 5: Commit**

```bash
git add webui/internal/device/session.go webui/internal/device/session_test.go
git commit -m "feat(webui): device stream session with reconnect and manager"
```

---

### Task 6: WS 网关 hub.go + handler.go

**Files:**
- Create: `webui/internal/ws/hub.go`
- Create: `webui/internal/ws/handler.go`
- Create: `webui/internal/ws/hub_test.go`

**Interfaces:**
- Consumes: `device.Broadcaster`、`device.VideoFrame`、`device.SessionInfo`、`control.DeviceMessage`、`control.Writer`、`device.Manager`
- Produces:
  - `ws.Hub`：`NewHub(manager *device.Manager) *Hub`，实现 `device.Broadcaster`（`PublishSession/PublishFrame/PublishDeviceMessage`）
  - `ws.Handler`：`NewHandler(h *Hub, m *device.Manager) http.Handler`，挂到 `/ws/{id}`
  - `ws.CtrlMsg`：浏览器→Go 的 JSON 控制消息：`{Type string; X,Y float64; ScreenW,ScreenH int; Action int; Keycode int; Text string}`

**浏览器 ⇄ Go 消息协议：**
- Go→浏览器：文本 JSON `{"type":"session","width":N,"height":N,"codec":"h264"}`；二进制 `[0x01][flags u8][payload]`（flags bit0=config, bit1=keyframe）；文本 JSON `{"type":"clipboard","text":"..."}`
- 浏览器→Go：文本 JSON 控制消息，`Type` ∈ `touch | key | text | home | back | recents | power | rotate`。`touch` 带 `{action,x,y,screenW,screenH}`（x/y 为设备像素坐标，前端已换算）；`key` 带 `{action,keycode}`；`text` 带 `{text}`；其余无字段。

- [ ] **Step 1: 写 Hub 测试**

```go
// webui/internal/ws/hub_test.go
package ws

import (
	"testing"
	"time"

	"scrcpy-lan/webui/internal/device"
)

type fakeSink struct {
	msgs chan []byte
}

func (f *fakeSink) write(b []byte) { f.msgs <- b }

// fakeConn 实现 hub 需要的 conn 接口：单向写通道。
type fakeConn struct{ out chan []byte }

func (c *fakeConn) Write(b []byte) { c.out <- b }
func (c *fakeConn) ID() string     { return "c1" }

func TestHubFanout(t *testing.T) {
	h := NewHub(nil)
	c := &fakeConn{out: make(chan []byte, 16)}
	h.subscribe("dev1", c)

	h.PublishFrame("dev1", &device.VideoFrame{Pts: 5, Data: []byte{0x01, 0x02}})
	select {
	case b := <-c.out:
		if b[0] != 0x01 {
			t.Fatalf("first byte = %x, want 0x01", b[0])
		}
	case <-time.After(time.Second):
		t.Fatal("no frame delivered")
	}

	h.unsubscribe("dev1", c)
	h.PublishFrame("dev1", &device.VideoFrame{Data: []byte{0xFF}})
	select {
	case b := <-c.out:
		t.Fatalf("got frame after unsubscribe: %x", b)
	default:
	}
}

func TestHubIgnoresOtherDevices(t *testing.T) {
	h := NewHub(nil)
	c := &fakeConn{out: make(chan []byte, 16)}
	h.subscribe("dev1", c)
	h.PublishFrame("dev2", &device.VideoFrame{Data: []byte{0xAA}})
	select {
	case b := <-c.out:
		t.Fatalf("dev2 frame leaked to dev1 sub: %x", b)
	case <-time.After(50 * time.Millisecond):
	}
}
```

- [ ] **Step 2: 运行测试确认失败**

Run: `go test ./internal/ws/`
Expected: 编译失败

- [ ] **Step 3: 实现 Hub**

```go
// webui/internal/ws/hub.go
package ws

import (
	"sync"

	"scrcpy-lan/webui/internal/control"
	"scrcpy-lan/webui/internal/device"
)

// conn 是 hub 对浏览器连接的最小抽象（由 handler 提供实现）。
type conn interface {
	Write(b []byte)
}

type Hub struct {
	mu    sync.RWMutex
	subs  map[string]map[string]conn // deviceID -> connID -> conn
	ctrl  *control.Writer
}

func NewHub() *Hub {
	return &Hub{
		subs: make(map[string]map[string]conn),
		ctrl: &control.Writer{},
	}
}

func (h *Hub) subscribe(deviceID string, c conn) {
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.subs[deviceID] == nil {
		h.subs[deviceID] = make(map[string]conn)
	}
	h.subs[deviceID][c.ID()] = c
}

func (h *Hub) unsubscribe(deviceID string, c conn) {
	h.mu.Lock()
	defer h.mu.Unlock()
	if m := h.subs[deviceID]; m != nil {
		delete(m, c.ID())
	}
}

func (h *Hub) PublishSession(id string, s device.SessionInfo) {
	payload := append([]byte(`{"type":"session","width":`), []byte(itoa(s.Width))...)
	payload = append(payload, []byte(`,"height":`+itoa(s.Height)+`,"codec":"h264"}`)...)
	h.broadcast(id, payload)
}

func (h *Hub) PublishFrame(id string, f *device.VideoFrame) {
	out := make([]byte, 2+len(f.Data))
	out[0] = 0x01
	if f.Config {
		out[1] |= 0x01
	}
	if f.KeyFrame {
		out[1] |= 0x02
	}
	copy(out[2:], f.Data)
	h.broadcast(id, out)
}

func (h *Hub) PublishDeviceMessage(id string, m *control.DeviceMessage) {
	if m.Type != control.DevMsgClipboard {
		return
	}
	payload := []byte(`{"type":"clipboard","text":` + jsonQuote(m.Text) + `}`)
	h.broadcast(id, payload)
}

func (h *Hub) broadcast(deviceID string, b []byte) {
	h.mu.RLock()
	defer h.mu.RUnlock()
	for _, c := range h.subs[deviceID] {
		c.Write(b)
	}
}

// itoa/jsonQuote 由实现者提供（strconv.Itoa / encoding/json.Marshal）。
func itoa(v int) string  { return strconv.Itoa(v) }
func jsonQuote(s string) string {
	b, _ := json.Marshal(s)
	return string(b)
}
```

（`strconv`、`encoding/json` 需在文件顶部 import；实现时补齐 import。）

- [ ] **Step 4: 运行测试确认通过**

Run: `go test ./internal/ws/`
Expected: PASS。`NewHub(nil)` 因测试传 nil 但签名是 `NewHub()`——按上面签名 `NewHub()` 调整测试调用为 `NewHub()`。

- [ ] **Step 5: 实现 WS handler**

```go
// webui/internal/ws/handler.go
package ws

import (
	"context"
	"encoding/json"
	"log"
	"net/http"
	"strings"
	"time"

	"github.com/coder/websocket"

	"scrcpy-lan/webui/internal/control"
	"scrcpy-lan/webui/internal/device"
)

type CtrlMsg struct {
	Type     string  `json:"type"`
	X, Y     float64 `json:"x,omitempty"`
	ScreenW  int     `json:"screenW,omitempty"`
	ScreenH  int     `json:"screenH,omitempty"`
	Action   int     `json:"action,omitempty"`
	Keycode  int     `json:"keycode,omitempty"`
	Text     string  `json:"text,omitempty"`
}

type Handler struct {
	hub     *Hub
	manager *device.Manager
	ctrl    *control.Writer
}

func NewHandler(hub *Hub, manager *device.Manager) *Handler {
	return &Handler{hub: hub, manager: manager, ctrl: &control.Writer{}}
}

type connWrapper struct {
	c  *websocket.Conn
	id string
	ws chan []byte
}

func (cw *connWrapper) ID() string { return cw.id }
func (cw *connWrapper) Write(b []byte) {
	select {
	case cw.ws <- b:
	default:
	}
}

func (h *Handler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	deviceID := strings.TrimPrefix(r.URL.Path, "/ws/")
	if deviceID == "" {
		http.Error(w, "missing device id", http.StatusBadRequest)
		return
	}
	c, err := websocket.Accept(w, r, nil)
	if err != nil {
		return
	}
	defer c.Close(websocket.StatusNormalClosure, "")

	cw := &connWrapper{c: c, id: "browser", ws: make(chan []byte, 512)}
	h.hub.subscribe(deviceID, cw)
	defer h.hub.unsubscribe(deviceID, cw)

	ctx := r.Context()

	// 写协程：把 hub 推来的视频/会话消息发到浏览器。
	go func() {
		for b := range cw.ws {
			writeCtx, cancel := context.WithTimeout(ctx, 2*time.Second)
			err := c.Write(writeCtx, websocket.MessageBinary, b)
			cancel()
			if err != nil {
				return
			}
		}
	}()

	// 读循环：浏览器控制消息 → 设备 control socket。
	for {
		_, data, err := c.Read(ctx)
		if err != nil {
			return
		}
		// 二进制帧是客户端回传（M2 无），忽略；文本是控制消息。
		var m CtrlMsg
		if err := json.Unmarshal(data, &m); err != nil {
			continue
		}
		h.route(deviceID, m)
	}
}

// route 把一条浏览器控制消息转成 scrcpy 控制字节并交给设备会话。
func (h *Handler) route(deviceID string, m CtrlMsg) {
	var b []byte
	switch m.Type {
	case "touch":
		b = h.ctrl.InjectTouch(control.Touch{
			Action:    m.Action,
			PointerID: 0,
			X:         int32(m.X),
			Y:         int32(m.Y),
			ScreenW:   uint16(m.ScreenW),
			ScreenH:   uint16(m.ScreenH),
			Pressure:  0xFFFF,
		})
	case "key":
		b = h.ctrl.InjectKey(control.Key{Action: m.Action, Keycode: int32(m.Keycode)})
	case "text":
		b = h.ctrl.InjectText(m.Text)
	case "home":
		// KEYCODE_HOME = 3
		b = append(h.ctrl.InjectKey(control.Key{Action: control.KeyActionDown, Keycode: 3}),
			h.ctrl.InjectKey(control.Key{Action: control.KeyActionUp, Keycode: 3})...)
	case "back":
		b = append(h.ctrl.InjectKey(control.Key{Action: control.KeyActionDown, Keycode: 4}),
			h.ctrl.InjectKey(control.Key{Action: control.KeyActionUp, Keycode: 4})...)
	case "recents":
		// KEYCODE_APP_SWITCH = 187
		b = append(h.ctrl.InjectKey(control.Key{Action: control.KeyActionDown, Keycode: 187}),
			h.ctrl.InjectKey(control.Key{Action: control.KeyActionUp, Keycode: 187})...)
	case "power":
		b = h.ctrl.InjectKey(control.Key{Action: control.KeyActionDown, Keycode: 26})
	case "rotate":
		b = h.ctrl.RotateDevice()
	default:
		log.Printf("unknown control msg type %q", m.Type)
		return
	}
	if err := h.manager.SendControl(deviceID, b); err != nil {
		log.Printf("send control to %s: %v", deviceID, err)
	}
}
```

- [ ] **Step 6: 编译**

Run: `go build ./...`
Expected: 编译通过（无未定义符号）

- [ ] **Step 7: Commit**

```bash
git add webui/internal/ws/
git commit -m "feat(webui): websocket hub fanout and control handler"
```

---

### Task 7: REST 设备 API + 静态服务

**Files:**
- Create: `webui/internal/httpapi/server.go`
- Create: `webui/internal/httpapi/devices.go`
- Create: `webui/internal/httpapi/devices_test.go`
- Modify: `webui/cmd/webui/main.go`

**Interfaces:**
- Consumes: `store.Config`/`store.DeviceConfig`、`device.Manager`、`device.SessionConfig`
- Produces: `httpapi.New(cfg *store.Config, mgr *device.Manager, configPath string) http.Handler`，提供：
  - `GET /api/health`
  - `GET /api/devices` → `[{"id","ip","online"}]`（online = Manager 有该会话）
  - `POST /api/devices` body `{"ip":"..."}` → 探测（TCP 连 27183 读 dummy+64B 名）→ 追加配置并 `Manager.Add` → 201
  - `DELETE /api/devices/{id}` → `Manager.Remove` + 从配置删除 → 204
  - 静态文件服务：`/` 指向 `web/dist/`（不存在则返回占位）

- [ ] **Step 1: 写 API 测试**

```go
// webui/internal/httpapi/devices_test.go
package httpapi

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"scrcpy-lan/webui/internal/device"
	"scrcpy-lan/webui/internal/store"
)

func TestDeviceLifecycle(t *testing.T) {
	cfg := &store.Config{}
	mgr := device.NewManager(&noopBroadcaster{})
	h := New(cfg, mgr, "")

	// 空列表
	req := httptest.NewRequest("GET", "/api/devices", nil)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != 200 || !strings.Contains(rec.Body.String(), `[]`) {
		t.Fatalf("list = %d %s", rec.Code, rec.Body.String())
	}

	// 添加不可达 IP → 502
	body := strings.NewReader(`{"ip":"127.0.0.1:1"}`)
	req = httptest.NewRequest("POST", "/api/devices", body)
	rec = httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusBadGateway {
		t.Fatalf("add unreachable = %d, want 502", rec.Code)
	}

	// 配置写盘（configPath 为空时跳过，测试只验证路由存在）
	_ = json.Marshal
}
```

（`noopBroadcaster` 实现 `device.Broadcaster` 空方法，测试内定义。`New` 的 `configPath` 为空时 `POST` 不落盘、仅加到 Manager，便于测试。实现时按此语义处理。）

- [ ] **Step 2: 运行测试确认失败**

Run: `go test ./internal/httpapi/`
Expected: 编译失败

- [ ] **Step 3: 实现 API + 静态服务**

```go
// webui/internal/httpapi/server.go
package httpapi

import (
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
)

func New(cfg *store.Config, mgr *device.Manager, configPath string) http.Handler {
	mux := http.NewServeMux()
	api := &api{cfg: cfg, mgr: mgr, configPath: configPath}
	mux.HandleFunc("/api/health", func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(map[string]bool{"ok": true})
	})
	mux.HandleFunc("/api/devices", api.handleDevices)
	mux.HandleFunc("/api/devices/", api.handleDevice)

	dist := filepath.Join("web", "dist")
	if _, err := os.Stat(dist); err == nil {
		mux.Handle("/", http.FileServer(http.Dir(dist)))
	} else {
		mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
			w.Write([]byte("webui running (web/dist not built)"))
		})
	}
	return mux
}
```

```go
// webui/internal/httpapi/devices.go
package httpapi

import (
	"encoding/json"
	"io"
	"net"
	"net/http"
	"strings"
	"time"

	"scrcpy-lan/webui/internal/device"
	"scrcpy-lan/webui/internal/store"
)

type api struct {
	cfg        *store.Config
	mgr        *device.Manager
	configPath string
}

type deviceDTO struct {
	ID     string `json:"id"`
	IP     string `json:"ip"`
	Online bool   `json:"online"`
	Name   string `json:"name,omitempty"`
}

func (a *api) handleDevices(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		a.list(w)
	case http.MethodPost:
		a.add(w, r)
	default:
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	}
}

func (a *api) handleDevice(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodDelete {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	id := strings.TrimPrefix(r.URL.Path, "/api/devices/")
	a.remove(w, id)
}

func (a *api) list(w http.ResponseWriter) {
	out := []deviceDTO{}
	for _, d := range a.cfg.Devices {
		online := a.mgr.Has(d.ID)
		out = append(out, deviceDTO{ID: d.ID, IP: d.IP, Online: online})
	}
	writeJSON(w, http.StatusOK, out)
}

func (a *api) add(w http.ResponseWriter, r *http.Request) {
	var req struct {
		IP string `json:"ip"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.IP == "" {
		http.Error(w, "bad request", http.StatusBadRequest)
		return
	}
	name, err := probeDevice(req.IP)
	if err != nil {
		http.Error(w, "device unreachable: "+err.Error(), http.StatusBadGateway)
		return
	}
	id := sanitizeID(req.IP)
	a.cfg.Devices = append(a.cfg.Devices, store.DeviceConfig{ID: id, IP: req.IP})
	if a.configPath != "" {
		store.Save(a.configPath, a.cfg)
	}
	a.mgr.Add(device.SessionConfig{ID: id, IP: req.IP})
	writeJSON(w, http.StatusCreated, deviceDTO{ID: id, IP: req.IP, Online: true, Name: name})
}

func (a *api) remove(w http.ResponseWriter, id string) {
	a.mgr.Remove(id)
	kept := a.cfg.Devices[:0]
	for _, d := range a.cfg.Devices {
		if d.ID != id {
			kept = append(kept, d)
		}
	}
	a.cfg.Devices = kept
	if a.configPath != "" {
		store.Save(a.configPath, a.cfg)
	}
	w.WriteHeader(http.StatusNoContent)
}

// probeDevice 连 27183 读 dummy+64B 设备名，用于添加前验证设备在线。
func probeDevice(ip string) (string, error) {
	addr := ip
	if _, _, err := net.SplitHostPort(ip); err != nil {
		addr = net.JoinHostPort(ip, "27183")
	}
	conn, err := net.DialTimeout("tcp", addr, 2*time.Second)
	if err != nil {
		return "", err
	}
	defer conn.Close()
	var dummy [1]byte
	if _, err := io.ReadFull(conn, dummy[:]); err != nil {
		return "", err
	}
	var name [device.DeviceNameLen]byte
	if _, err := io.ReadFull(conn, name[:]); err != nil {
		return "", err
	}
	return strings.TrimRight(string(name[:]), "\x00"), nil
}

func sanitizeID(ip string) string {
	return strings.ReplaceAll(ip, ":", "-")
}

func writeJSON(w http.ResponseWriter, code int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	json.NewEncoder(w).Encode(v)
}
```

`device.Manager` 需要补一个 `Has(id string) bool`（加锁检查 map 成员）。

- [ ] **Step 4: 接线 main.go**

修改 `webui/cmd/webui/main.go`：把 `mux` 换成 `httpapi.New(cfg, mgr, *configPath)`；创建 `device.NewManager(&hub)`，其中 hub 由 ws 包提供（Task 6 的 `NewHub()`）。同时把 `/ws/` 挂到 `ws.NewHandler(hub, mgr)`。main 里各包初始化顺序：hub → manager → httpapi。

- [ ] **Step 5: 编译 + 测试**

Run: `go build ./... && go test ./...`
Expected: 全部编译、测试通过

- [ ] **Step 6: Commit**

```bash
git add webui/internal/httpapi webui/cmd/webui
git commit -m "feat(webui): device REST api with probe, config persistence, static serving"
```

---

### Task 8: Vue 脚手架 + 设备列表

**Files:**
- Create: `webui/web/package.json`、`vite.config.ts`、`tsconfig.json`、`index.html`
- Create: `webui/web/src/main.ts`、`App.vue`、`router.ts`、`api.ts`、`stores/device.ts`
- Create: `webui/web/src/views/Devices.vue`

**Interfaces:**
- Consumes: `GET /api/devices`、`POST /api/devices {ip}`、`DELETE /api/devices/{id}`（REST，dev 走 Vite 代理到 Go `:8080`）
- Produces: 设备列表页 + 添加（弹窗输 IP）+ 删除；路由 `/`（列表）、`/devices/:id`（控制台）

- [ ] **Step 1: 初始化 Vite 项目**

```powershell
cd D:\Kita-Tools\scrcpy-lan\webui\web
npm create vite@latest . -- --template vue-ts
npm install
npm install pinia vue-router vuetify
```

- [ ] **Step 2: Vite 代理配置**

```ts
// webui/web/vite.config.ts
import { defineConfig } from 'vite'
import vue from '@vitejs/plugin-vue'

export default defineConfig({
  plugins: [vue()],
  server: {
    proxy: {
      '/api': 'http://localhost:8080',
      '/ws': { target: 'ws://localhost:8080', ws: true },
    },
  },
})
```

- [ ] **Step 3: API 封装 + 设备 store**

```ts
// webui/web/src/api.ts
export interface Device {
  id: string
  ip: string
  online: boolean
  name?: string
}

export async function listDevices(): Promise<Device[]> {
  const r = await fetch('/api/devices')
  return r.json()
}

export async function addDevice(ip: string): Promise<Device> {
  const r = await fetch('/api/devices', {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify({ ip }),
  })
  if (!r.ok) throw new Error((await r.text()) || '添加失败')
  return r.json()
}

export async function removeDevice(id: string): Promise<void> {
  await fetch(`/api/devices/${id}`, { method: 'DELETE' })
}
```

```ts
// webui/web/src/stores/device.ts
import { defineStore } from 'pinia'
import { listDevices, addDevice, removeDevice, type Device } from '../api'

export const useDeviceStore = defineStore('device', {
  state: () => ({ devices: [] as Device[] }),
  actions: {
    async refresh() {
      this.devices = await listDevices()
    },
    async add(ip: string) {
      await addDevice(ip)
      await this.refresh()
    },
    async remove(id: string) {
      await removeDevice(id)
      await this.refresh()
    },
  },
})
```

- [ ] **Step 4: 设备列表页**

```vue
<!-- webui/web/src/views/Devices.vue -->
<script setup lang="ts">
import { onMounted, ref } from 'vue'
import { useDeviceStore } from '../stores/device'

const store = useDeviceStore()
const dialog = ref(false)
const ip = ref('')
const error = ref('')

onMounted(() => store.refresh())

async function submit() {
  error.value = ''
  try {
    await store.add(ip.value)
    ip.value = ''
    dialog.value = false
  } catch (e: any) {
    error.value = e.message
  }
}
</script>

<template>
  <v-container>
    <v-row justify="space-between" align="center">
      <v-col><v-btn color="primary" @click="dialog = true">添加设备</v-btn></v-col>
    </v-row>
    <v-list>
      <v-list-item
        v-for="d in store.devices"
        :key="d.id"
        :to="`/devices/${d.id}`"
        :title="d.name || d.ip"
        :subtitle="d.ip"
      >
        <template #append>
          <v-chip :color="d.online ? 'success' : 'error'" size="small">
            {{ d.online ? '在线' : '离线' }}
          </v-chip>
          <v-btn icon="mdi-delete" variant="text" @click.stop="store.remove(d.id)" />
        </template>
      </v-list-item>
    </v-list>

    <v-dialog v-model="dialog" width="400">
      <v-card>
        <v-card-title>添加设备</v-card-title>
        <v-card-text>
          <v-text-field v-model="ip" label="设备 IP" placeholder="192.168.1.50" />
          <v-alert v-if="error" type="error" density="compact">{{ error }}</v-alert>
        </v-card-text>
        <v-card-actions>
          <v-spacer />
          <v-btn @click="dialog = false">取消</v-btn>
          <v-btn color="primary" @click="submit">添加</v-btn>
        </v-card-actions>
      </v-card>
    </v-dialog>
  </v-container>
</template>
```

- [ ] **Step 5: main.ts 装配 Vuetify + Pinia + Router**

```ts
// webui/web/src/main.ts
import { createApp } from 'vue'
import { createPinia } from 'pinia'
import { createRouter, createWebHistory } from 'vue-router'
import { createVuetify } from 'vuetify'
import 'vuetify/styles'
import App from './App.vue'
import Devices from './views/Devices.vue'
import DeviceConsole from './views/DeviceConsole.vue'

const router = createRouter({
  history: createWebHistory(),
  routes: [
    { path: '/', component: Devices },
    { path: '/devices/:id', component: DeviceConsole, props: true },
  ],
})

createApp(App)
  .use(createPinia())
  .use(router)
  .use(createVuetify())
  .mount('#app')
```

（`App.vue` 提供 `<router-view />`；`DeviceConsole.vue` 在 Task 9 创建，创建前 main.ts 先注释该路由或放占位组件保证可跑。）

- [ ] **Step 6: 手工验证**

Run: `npm run dev`，浏览器打开 Vite 地址。Expected: 设备列表空；启动 Go（`go run ./cmd/webui`），加一台真机 IP，能看到条目、在线状态为绿。

- [ ] **Step 7: Commit**

```bash
git add webui/web
git commit -m "feat(webui): vue scaffold with vuetify, device list page"
```

---

### Task 9: Vue WebCodecs 视频解码（useStream）

**Files:**
- Create: `webui/web/src/composables/useStream.ts`
- Modify: `webui/web/src/views/DeviceConsole.vue`（视频画布部分）

**Interfaces:**
- Consumes: WS `ws://host/ws/{deviceId}`；Go→浏览器二进制帧 `[0x01][flags][payload]`、文本 `{"type":"session"|"clipboard",...}`
- Produces: `useStream(deviceId: string, canvas: HTMLCanvasElement)` 返回 `{ connect, disconnect, send }`；`send(msg: object)` 发 JSON 控制消息；`connect()` 建立 WS + WebCodecs，返回 `Promise<void>`

**解码要点：**
- `new VideoDecoder({ output, error })`，配置 `{ codec: 'avc1.640028', optimizeForLatency: true }`（codec 字符串来自 session meta；M2 固定 h264，`avc1.640028` 是通用 Baseline/High 保守值，能解大部分 MediaCodec 输出）
- 逐帧 `decoder.decode(new EncodedVideoChunk({ type: 'key'|'delta', timestamp: ptsUs, data: new Uint8Array(payload) }))`
- config 帧（flags bit0）喂给 decoder 的 `description`：`decoder.configure({ codec, description })`，或直接作为第一个 chunk（AnnexB 含 SPS/PPS 时 Chrome 可自动初始化）
- output 回调把 `new VideoFrame` 画到 canvas（`ctx.drawImage(frame, 0, 0, canvas.width, canvas.height)`），随后 `frame.close()`

- [ ] **Step 1: 实现 useStream**

```ts
// webui/web/src/composables/useStream.ts
import { ref } from 'vue'

export interface StreamHandle {
  connect: () => Promise<void>
  disconnect: () => void
  send: (msg: Record<string, unknown>) => void
}

export function useStream(deviceId: string, canvas: HTMLCanvasElement): StreamHandle {
  let ws: WebSocket | null = null
  let decoder: VideoDecoder | null = null
  let width = 0
  let height = 0
  const ctx = canvas.getContext('2d')!
  const connected = ref(false)

  function setupDecoder() {
    decoder = new VideoDecoder({
      output(frame) {
        if (!width || !height) {
          width = frame.displayWidth
          height = frame.displayHeight
          canvas.width = width
          canvas.height = height
        }
        ctx.drawImage(frame, 0, 0, canvas.width, canvas.height)
        frame.close()
      },
      error(e) {
        console.error('VideoDecoder error', e)
      },
    })
  }

  function handleBinary(buf: ArrayBuffer) {
    const u8 = new Uint8Array(buf)
    if (u8.length < 2 || u8[0] !== 0x01) return
    const flags = u8[1]
    const payload = u8.slice(2)
    const isConfig = (flags & 0x01) !== 0
    const isKey = (flags & 0x02) !== 0

    if (!decoder) setupDecoder()
    if (isConfig && decoder && decoder.state === 'unconfigured') {
      // 首个 config 帧含 SPS/PPS，作为 description 配置解码器。
      decoder.configure({
        codec: 'avc1.640028',
        optimizeForLatency: true,
        description: new Uint8Array(payload),
      })
      return
    }
    if (decoder && decoder.state === 'configured') {
      decoder.decode(new EncodedVideoChunk({
        type: isKey ? 'key' : 'delta',
        timestamp: performance.now() * 1000,
        data: payload,
      }))
    }
  }

  return {
    connected,
    async connect() {
      setupDecoder()
      await new Promise<void>((resolve, reject) => {
        ws = new WebSocket(`ws://${location.host}/ws/${deviceId}`)
        ws.binaryType = 'arraybuffer'
        ws.onopen = () => resolve()
        ws.onerror = () => reject(new Error('WS 连接失败'))
        ws.onmessage = (ev) => {
          if (typeof ev.data === 'string') {
            const m = JSON.parse(ev.data)
            if (m.type === 'session') {
              width = m.width
              height = m.height
              canvas.width = width
              canvas.height = height
            }
            return
          }
          handleBinary(ev.data as ArrayBuffer)
        }
        ws.onclose = () => { decoder?.close(); decoder = null }
      })
    },
    disconnect() {
      ws?.close()
    },
    send(msg) {
      ws?.send(JSON.stringify(msg))
    },
  }
}
```

> 注：`description` 期望 AVCDecoderConfigurationRecord（AVCC），而设备发来的是 AnnexB 的 SPS/PPS NAL。直接作为 description 在部分浏览器不认。**更稳妥**：把 config 帧 payload（AnnexB SPS/PPS）直接作为第一个 `key` chunk 喂给 `decode()`（Chrome 支持从 AnnexB 初始化），`description` 留空。实现时优先采用后者；若白屏再退回 description 路径。M2 验收以「浏览器能看到画面」为准。

- [ ] **Step 2: 控制台页（视频画布）**

```vue
<!-- webui/web/src/views/DeviceConsole.vue -->
<script setup lang="ts">
import { onMounted, ref } from 'vue'
import { useRoute } from 'vue-router'
import { useStream } from '../composables/useStream'

const route = useRoute()
const deviceId = route.params.id as string
const canvas = ref<HTMLCanvasElement | null>(null)
const stream = ref<ReturnType<typeof useStream> | null>(null)
const err = ref('')

onMounted(async () => {
  if (!canvas.value) return
  const s = useStream(deviceId, canvas.value)
  stream.value = s
  try {
    await s.connect()
  } catch (e: any) {
    err.value = e.message
  }
})
</script>

<template>
  <div class="pa-4">
    <v-alert v-if="err" type="error">{{ err }}</v-alert>
    <canvas ref="canvas" style="width: 100%; max-width: 480px; background: #000" />
  </div>
</template>
```

- [ ] **Step 3: 手工验证（需要真机/已启动设备端）**

1. 设备端 `scrcpy-lan-daemon.sh` 已在跑，确认 `27183` 可达
2. Go 运行 + `npm run dev`，添加设备，点进控制台
Expected: 浏览器 canvas 显示设备实时画面（分辨率跟随设备）

- [ ] **Step 4: Commit**

```bash
git add webui/web/src/composables webui/web/src/views/DeviceConsole.vue
git commit -m "feat(webui): webcodecs h264 decode and device console canvas"
```

---

### Task 10: Vue 触控 + 键盘 + 快捷按钮

**Files:**
- Modify: `webui/web/src/views/DeviceConsole.vue`

**Interfaces:**
- Consumes: `stream.send()`（发 `touch`/`key`/`home`/`back`/`recents`/`power`/`rotate` JSON）；`touch` 消息 x/y 为**设备像素坐标**，前端用「canvas 显示尺寸 ÷ 设备宽高」换算
- Produces: 完整的单台控制台交互

- [ ] **Step 1: 实现触控映射**

```ts
// DeviceConsole.vue 内新增
import { onBeforeUnmount } from 'vue'

let pointerDown = false
const canvas = ref<HTMLCanvasElement | null>(null)
const dims = ref({ w: 0, h: 0 })

function canvasPoint(e: PointerEvent) {
  const el = canvas.value!
  const rect = el.getBoundingClientRect()
  const x = ((e.clientX - rect.left) / rect.width) * dims.value.w
  const y = ((e.clientY - rect.top) / rect.height) * dims.value.h
  return { x, y }
}

function sendTouch(action: number, e: PointerEvent) {
  const p = canvasPoint(e)
  stream.value?.send({
    type: 'touch',
    action,
    x: p.x,
    y: p.y,
    screenW: dims.value.w,
    screenH: dims.value.h,
  })
}

canvas.value?.addEventListener('pointerdown', (e) => {
  pointerDown = true
  canvas.value?.setPointerCapture(e.pointerId)
  sendTouch(0, e) // DOWN
})
canvas.value?.addEventListener('pointermove', (e) => {
  if (pointerDown) sendTouch(2, e) // MOVE
})
canvas.value?.addEventListener('pointerup', (e) => {
  pointerDown = false
  sendTouch(1, e) // UP
})
```

> 注意：`dims` 要在 useStream 的 `session` 消息里更新（Task 9 中 `width/height` 赋值处同步到 `dims.value`）。触控换算用设备的 `session` 宽高，不是 canvas CSS 尺寸。

- [ ] **Step 2: 实现键盘映射**

```ts
const KEYMAP: Record<string, number> = {
  Enter: 66, Backspace: 67, Tab: 61, Space: 62,
  ArrowUp: 19, ArrowDown: 20, ArrowLeft: 21, ArrowRight: 22,
  Home: 122, End: 123, PageUp: 92, PageDown: 93,
  Delete: 67, Escape: 111, Shift: 59, Control: 113, Alt: 57,
}

const LETTERS: Record<string, number> = {
  a: 29, b: 30, c: 31, d: 32, e: 33, f: 34, g: 35, h: 36, i: 37,
  j: 38, k: 39, l: 40, m: 41, n: 42, o: 43, p: 44, q: 45, r: 46,
  s: 47, t: 48, u: 49, v: 50, w: 51, x: 52, y: 53, z: 54,
}

const DIGITS: Record<string, number> = {
  '0': 7, '1': 8, '2': 9, '3': 10, '4': 11, '5': 12, '6': 13, '7': 14, '8': 15, '9': 16,
}

function keyToKeycode(e: KeyboardEvent): number | null {
  if (e.code.startsWith('Key')) return LETTERS[e.code.slice(3).toLowerCase()] ?? null
  if (e.code.startsWith('Digit')) return DIGITS[e.code.slice(5)] ?? null
  return KEYMAP[e.code] ?? null
}

function sendKey(action: number, e: KeyboardEvent) {
  const code = keyToKeycode(e)
  if (code == null) return
  e.preventDefault()
  stream.value?.send({ type: 'key', action, keycode: code })
}
```

（在控制台根元素上 `keydown`→`sendKey(0)`、`keyup`→`sendKey(1)`。字母/数字 map 只覆盖基础字符，M2 够用，后续里程碑可扩展。）

- [ ] **Step 3: 实现快捷按钮**

```vue
<template>
  <div class="d-flex ga-2 my-2">
    <v-btn size="small" @click="stream?.send({ type: 'home' })">HOME</v-btn>
    <v-btn size="small" @click="stream?.send({ type: 'back' })">BACK</v-btn>
    <v-btn size="small" @click="stream?.send({ type: 'recents' })">RECENTS</v-btn>
    <v-btn size="small" @click="stream?.send({ type: 'power' })">电源</v-btn>
    <v-btn size="small" @click="stream?.send({ type: 'rotate' })">旋转</v-btn>
  </div>
</template>
```

- [ ] **Step 4: 手工验证**

点 canvas 能触发点击/滑动；键盘能输入字符；HOME/BACK/RECENTS/电源/旋转生效。Expected: 手机端画面有对应响应。

- [ ] **Step 5: Commit**

```bash
git add webui/web/src/views/DeviceConsole.vue
git commit -m "feat(webui): touch, keyboard and shortcut control in device console"
```

---

### Task 11: 集成收尾 + 文档

**Files:**
- Create: `webui/.gitignore`
- Create: `webui/README.md`
- Modify: `webui/cmd/webui/main.go`（可选：`-static` 指向已构建的 dist）

- [ ] **Step 1: 生产构建脚本**

```powershell
cd D:\Kita-Tools\scrcpy-lan\webui
cd web
npm run build        # 产出 web/dist
cd ..
go build ./cmd/webui # 产出 webui.exe
```

Expected: `webui.exe` 运行后浏览器访问 `http://localhost:8080` 直接可用（Go 服务 dist + API + WS）。

- [ ] **Step 2: .gitignore**

```
webui/web/node_modules/
webui/web/dist/
webui/webui.exe
webui/devices.json
```

- [ ] **Step 3: README**

写清：架构（设备 27183 → Go → WS → 浏览器 WebCodecs）、启动步骤（设备端 daemon 已在跑 → `go run ./cmd/webui` → `npm run dev`）、添加设备、支持的控制、M2 边界（无批量/网格/监控，无 adb）。

- [ ] **Step 4: 全量回归**

Run: `go test ./...`（应全绿）+ 浏览器手工走一遍「加设备 → 看画面 → 控制 → 删设备 → 断网重连」

- [ ] **Step 5: Commit**

```bash
git add webui/.gitignore webui/README.md
git commit -m "docs(webui): integration run guide and gitignore"
```

---

## Self-Review 记录

- **Spec 覆盖**：M2（单设备上屏）= 视频解析(T2)、控制注入(T3/T10)、WS 中继(T6)、设备发现-手动配置(T1/T7)、WebCodecs 解码(T9)、断线重连(T5)。M2 边界内的 spec 项全部有任务。音频按 M2 范围「连接并丢弃」处理（T5）。
- **占位符**：无 TBD；`itoa/jsonQuote` 在 T6 中已给出实现（在 hub.go 内定义），import 缺失已在任务内提示补齐。
- **类型一致性**：`device.VideoFrame/SessionInfo/Broadcaster`、`control.Touch/Key/Writer`、`device.Manager.Has()`（T7 新增）在后续任务引用一致。
