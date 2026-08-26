package httpapi

import (
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"strings"

	"scrcpy-lan/webui/internal/device"
	"scrcpy-lan/webui/internal/status"
	"scrcpy-lan/webui/internal/store"
)

func New(cfg *store.Config, mgr *device.Manager, configPath string, collector *status.Collector) http.Handler {
	mux := http.NewServeMux()
	api := &api{cfg: cfg, mgr: mgr, configPath: configPath, collector: collector}
	mux.HandleFunc("/api/health", func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(map[string]bool{"ok": true})
	})
	mux.HandleFunc("/api/devices", api.handleDevices)
	mux.HandleFunc("/api/devices/discover", api.handleDiscover)
	mux.HandleFunc("/api/devices/batch/", api.handleBatch)
	mux.HandleFunc("/api/devices/", api.handleDevice)

	dist := filepath.Join("web", "dist")
	if _, err := os.Stat(dist); err == nil {
		mux.Handle("/", spaHandler{dist: dist})
	} else {
		mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
			w.Write([]byte("webui running (web/dist not built)"))
		})
	}
	return mux
}

// spaHandler serves built static files and falls back to index.html so the
// client-side routes (/devices/:id) survive a browser refresh. API and WS
// paths are never rewritten.
type spaHandler struct {
	dist string
}

func (h spaHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if strings.HasPrefix(r.URL.Path, "/api/") || strings.HasPrefix(r.URL.Path, "/ws/") {
		http.NotFound(w, r)
		return
	}
	upath := r.URL.Path
	if !strings.HasPrefix(upath, "/") {
		upath = "/" + upath
	}
	full := filepath.Join(h.dist, filepath.FromSlash(upath))
	if fi, err := os.Stat(full); err == nil && !fi.IsDir() {
		http.ServeFile(w, r, full)
		return
	}
	http.ServeFile(w, r, filepath.Join(h.dist, "index.html"))
}
