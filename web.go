package main

import (
	"context"
	"encoding/json"
	"fmt"
	"html/template"
	"net/http"
	"strconv"

	_ "embed"
)

//go:embed index.html
var pageHTML string

var pageTemplate = template.Must(template.New("index").Parse(pageHTML))

type IndexPageData struct {
	SID         int64
	Speed       string
	StartupText string
}

func (a *App) handleIndex(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path != "/" {
		http.NotFound(w, r)
		return
	}

	w.Header().Set("Content-Type", "text/html; charset=utf-8")

	data := IndexPageData{
		SID:         a.sid.Load(),
		Speed:       fmt.Sprintf("%.02f", float64(a.speedX100.Load())/100),
		StartupText: a.cfg.StartupText,
	}

	if err := pageTemplate.Execute(w, data); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
}

func (a *App) handleSetSID(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		http.Error(w, "invalid form", http.StatusBadRequest)
		return
	}

	v, err := strconv.ParseInt(r.FormValue("sid"), 10, 64)
	if err != nil || v < 0 || v > maxSID {
		http.Error(w, "invalid speaker id", http.StatusBadRequest)
		return
	}

	a.sid.Store(v)
}

const maxSID = 904

func (a *App) handleSetSpeed(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		http.Error(w, "invalid form", http.StatusBadRequest)
		return
	}

	speed, err := strconv.ParseFloat(r.FormValue("speed"), 64)
	if err != nil || speed < 0.2 || speed > 4 {
		http.Error(w, "invalid speed", http.StatusBadRequest)
		return
	}

	a.speedX100.Store(int64(speed * 100))
}

func (a *App) handleStop(w http.ResponseWriter, r *http.Request) {
	a.mu.Lock()
	if a.cancel != nil {
		a.cancel()
		a.cancel = nil
	}
	a.mu.Unlock()

	// Invalidate currently running playback session.
	a.playSessionID.Add(1)
	a.progressPlaying.Store(false)
	a.currentSentence.Store("")
}

func (a *App) handlePlay(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		http.Error(w, "invalid form", http.StatusBadRequest)
		return
	}

	text := r.FormValue("text")
	if text == "" {
		http.Error(w, "empty text", 400)
		return
	}

	sentences := splitSentences(text)
	if len(sentences) == 0 {
		http.Error(w, "empty text", 400)
		return
	}

	totalChunks := int64(len(sentences))

	sessionID := a.playSessionID.Add(1)

	a.currentSentence.Store("")
	a.resetProgress(totalChunks)

	ctx, cancel := context.WithCancel(context.Background())

	a.mu.Lock()
	if a.cancel != nil {
		a.cancel()
	}
	a.cancel = cancel
	a.mu.Unlock()

	go a.playLoop(ctx, sessionID, sentences)

	w.Header().Set("Content-Type", "application/json")

	json.NewEncoder(w).Encode(PlayResponse{
		Text: text, // strings.Join(sentences, "\n"),
	})
}

type PlayResponse struct {
	Text string `json:"text"`
}
