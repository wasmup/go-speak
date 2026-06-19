package main

import (
	"encoding/json"
	"net/http"
)

func (a *App) resetProgress(total int64) {
	a.progressDone.Store(0)
	a.progressTotal.Store(total)
	a.progressPlaying.Store(total > 0)
}

func (a *App) handleProgress(w http.ResponseWriter, r *http.Request) {
	done := a.progressDone.Load()
	total := a.progressTotal.Load()
	playing := a.progressPlaying.Load()

	var percent int64
	if total > 0 {
		percent = done * 100 / total
	}

	resp := ProgressResponse{
		Done:    done,
		Total:   total,
		Playing: playing,
		Percent: percent,
	}
	if v := a.currentSentence.Load(); v != nil {
		resp.Current, _ = v.(string)
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(resp)
}

type ProgressResponse struct {
	Done    int64  `json:"done"`
	Total   int64  `json:"total"`
	Playing bool   `json:"playing"`
	Percent int64  `json:"percent"`
	Current string `json:"current"`
}
