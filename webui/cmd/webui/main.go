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
	_ = cfg

	mux := http.NewServeMux()
	mux.HandleFunc("/api/health", func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(map[string]bool{"ok": true})
	})

	log.Printf("webui listening on %s", *addr)
	log.Fatal(http.ListenAndServe(*addr, mux))
}
