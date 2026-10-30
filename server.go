package main

import (
	"net"
	"net/http"
	"net/url"
	"strings"
	"time"
)

func (a *App) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /{$}", a.handleIndex)
	mux.HandleFunc("POST /play", a.handlePlay)
	mux.HandleFunc("POST /pause", a.HandlePause)
	mux.HandleFunc("POST /stop", a.handleStop)
	mux.HandleFunc("GET /progress", a.handleProgress)
	mux.HandleFunc("POST /set_sid", a.handleSetSID)
	mux.HandleFunc("POST /set_speed", a.handleSetSpeed)
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "no-store")
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("X-Frame-Options", "DENY")
		w.Header().Set("Content-Security-Policy", "default-src 'self'; script-src 'self' 'unsafe-inline'; style-src 'self' 'unsafe-inline'; frame-ancestors 'none'; base-uri 'none'; form-action 'self'")
		// Reject DNS rebinding, cross-site form submissions, and fetches from
		// other local applications. CLI clients without Origin remain supported.
		host := r.Host
		if h, _, err := net.SplitHostPort(host); err == nil {
			host = h
		} else {
			host = strings.Trim(host, "[]")
		}
		ip := net.ParseIP(host)
		if host != "localhost" && (ip == nil || !ip.IsLoopback()) {
			http.Error(w, "invalid host", 403)
			return
		}
		site := r.Header.Get("Sec-Fetch-Site")
		if site != "" && site != "same-origin" && site != "none" {
			http.Error(w, "cross-origin request denied", 403)
			return
		}
		if origin := r.Header.Get("Origin"); origin != "" {
			u, err := url.Parse(origin)
			scheme := "http"
			if r.TLS != nil {
				scheme = "https"
			}
			if err != nil || u.Scheme != scheme || u.Host != r.Host || u.User != nil || u.Path != "" || u.RawQuery != "" || u.Fragment != "" {
				http.Error(w, "cross-origin request denied", 403)
				return
			}
		}
		r.Body = http.MaxBytesReader(w, r.Body, 2<<20)
		mux.ServeHTTP(w, r)
	})
}

func newServer(cfg *Config, a *App) *http.Server {
	return &http.Server{Addr: cfg.Addr, Handler: a.Handler(), ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout: 15 * time.Second, WriteTimeout: 120 * time.Second, IdleTimeout: 60 * time.Second, MaxHeaderBytes: 16 << 10}
}
