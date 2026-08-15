package httpapi

import (
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"

	"scrcpy-lan/webui/internal/device"
	"scrcpy-lan/webui/internal/store"
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
