package main

import (
	"log"
	"net/http"
)

func main() {
	cfg, err := ParseFlags()
	if err != nil {
		log.Fatal(err)
	}

	app, err := NewApp(cfg)
	if err != nil {
		log.Fatal(err)
	}

	mux := http.NewServeMux()
	srv := &http.Server{
		Addr:    cfg.Addr,
		Handler: mux,
	}

	mux.HandleFunc("GET /", app.handleIndex)
	mux.HandleFunc("POST /play", app.handlePlay)
	mux.HandleFunc("POST /stop", app.handleStop)
	mux.HandleFunc("GET /progress", app.handleProgress)
	mux.HandleFunc("POST /set_sid", app.handleSetSID)
	mux.HandleFunc("POST /set_speed", app.handleSetSpeed)

	log.Printf("Listening on %s", cfg.Addr)
	log.Fatal(srv.ListenAndServe())
}
