package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"net/http"
	"time"

	"scrcpy-lan/webui/internal/bus"
	"scrcpy-lan/webui/internal/control"
	"scrcpy-lan/webui/internal/device"
	"scrcpy-lan/webui/internal/httpapi"
	"scrcpy-lan/webui/internal/proxy"
	"scrcpy-lan/webui/internal/status"
	"scrcpy-lan/webui/internal/store"
	"scrcpy-lan/webui/internal/ws"
)

// broadcaster 组合 hub 与 bus：会话建立时把分辨率登记进主控-被控总线，
// 其余流消息原样转发给 hub。
type broadcaster struct {
	hub *ws.Hub
	bus *bus.Bus
}

func (b *broadcaster) PublishSession(id string, s device.SessionInfo) {
	b.hub.PublishSession(id, s)
	b.bus.RegisterDim(id, bus.Dimensions{W: s.Width, H: s.Height})
}

func (b *broadcaster) PublishFrame(id string, f *device.VideoFrame) {
	b.hub.PublishFrame(id, f)
}

func (b *broadcaster) PublishAudioFrame(id string, f *device.AudioFrame) {
	b.hub.PublishAudioFrame(id, f)
}

func (b *broadcaster) PublishDeviceMessage(id string, m *control.DeviceMessage) {
	b.hub.PublishDeviceMessage(id, m)
}

func main() {
	configPath := flag.String("config", "devices.json", "path to devices config file")
	addr := flag.String("addr", ":8080", "listen address")
	nativePort := flag.String("native-port", "27182", "native scrcpy protocol proxy port")
	flag.Parse()

	cfg, err := store.Load(*configPath)
	if err != nil {
		log.Printf("no config loaded (%v), starting empty", err)
		cfg = &store.Config{}
	}

	hub := ws.NewHub()
	b := bus.NewBus()
	mgr := device.NewManager(&broadcaster{hub: hub, bus: b})

	// 状态采集：定时采样 + 变化广播（status/alert）。
	collector := status.NewCollector(mgr)
	collector.SetOnChange(func(old, new status.Info) {
		payload, err := json.Marshal(map[string]any{
			"type":    "status",
			"id":      new.ID,
			"online":  new.Online,
			"battery": new.Battery,
			"plugged": new.Plugged,
			"model":   new.Model,
			"android": new.Android,
		})
		if err == nil {
			hub.PublishJSON(new.ID, payload)
		}
		if old.Online && !new.Online {
			publishAlert(hub, new.ID, "offline", fmt.Sprintf("设备 %s 掉线", new.ID))
		} else if !old.Online && new.Online {
			publishAlert(hub, new.ID, "online", fmt.Sprintf("设备 %s 上线", new.ID))
		}
		if new.Online && new.Battery >= 0 && new.Battery < 20 && (old.Battery == -1 || old.Battery >= 20) {
			publishAlert(hub, new.ID, "lowbattery", fmt.Sprintf("设备 %s 电量低: %d%%", new.ID, new.Battery))
		}
	})

	// 重启后把已持久化的设备重新拉起会话，避免全部停在离线态。
	ids := make([]string, 0, len(cfg.Devices))
	for _, d := range cfg.Devices {
		ids = append(ids, d.ID)
		mgr.Add(device.SessionConfig{ID: d.ID, IP: d.IP})
	}
	collector.Track(ids)
	collector.Start(5 * time.Second)

	if err := proxy.New(":"+*nativePort, mgr).Start(); err != nil {
		log.Printf("native proxy failed to start: %v", err)
	}
	apiHandler := httpapi.New(cfg, mgr, *configPath, collector)
	wsHandler := ws.NewHandler(hub, mgr, b)

	top := http.NewServeMux()
	top.Handle("/", apiHandler)
	top.Handle("/ws/", wsHandler)

	log.Printf("webui listening on %s", *addr)
	log.Fatal(http.ListenAndServe(*addr, top))
}

func publishAlert(hub *ws.Hub, id, level, msg string) {
	payload, err := json.Marshal(map[string]any{
		"type":  "alert",
		"id":    id,
		"level": level,
		"msg":   msg,
	})
	if err != nil {
		return
	}
	hub.PublishJSON(id, payload)
}
