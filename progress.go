package main

import (
	"encoding/json"
	"net/http"
)

func (a *App) handleProgress(w http.ResponseWriter, r *http.Request) {
	a.mu.Lock()
	p := a.progress
	a.mu.Unlock()
	if p.Total > 0 {
		p.Percent = p.Done * 100 / p.Total
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(p)
}

type ProgressResponse struct {
	Done     int64   `json:"done"`
	Total    int64   `json:"total"`
	Playing  bool    `json:"playing"`
	Paused   bool    `json:"paused"`
	Percent  int64   `json:"percent"`
	Current  string  `json:"current"`
	WPM      int     `json:"wpm"`
	Words    int     `json:"words"`
	Duration float64 `json:"duration"`
	Error    string  `json:"error,omitempty"`
}
