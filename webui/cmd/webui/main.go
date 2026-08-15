package main

import (
	"flag"
	"log"
	"net/http"

	"scrcpy-lan/webui/internal/device"
	"scrcpy-lan/webui/internal/httpapi"
	"scrcpy-lan/webui/internal/store"
	"scrcpy-lan/webui/internal/ws"
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

	hub := ws.NewHub()
	mgr := device.NewManager(hub)
	apiHandler := httpapi.New(cfg, mgr, *configPath)
	wsHandler := ws.NewHandler(hub, mgr)

	top := http.NewServeMux()
	top.Handle("/", apiHandler)
	top.Handle("/ws/", wsHandler)

	log.Printf("webui listening on %s", *addr)
	log.Fatal(http.ListenAndServe(*addr, top))
}
