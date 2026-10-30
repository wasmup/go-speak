package main

import (
	"context"
	_ "embed"
	"encoding/json"
	"fmt"
	"html/template"
	"log"
	"math"
	"net/http"
	"strconv"
	"strings"
	"unicode/utf8"
)

//go:embed index.html
var pageHTML string
var pageTemplate = template.Must(template.New("index").Parse(pageHTML))

type IndexPageData struct {
	Models      []string
	Index       int
	SID         int64
	Speed       string
	StartupText string
}

func (a *App) handleIndex(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path != "/" {
		http.NotFound(w, r)
		return
	}
	models := make([]string, len(a.cfg.Models))
	for i, m := range a.cfg.Models {
		models[i] = m.Name
	}
	a.mu.Lock()
	index := a.modelIndex
	a.mu.Unlock()
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	if err := pageTemplate.Execute(w, IndexPageData{Models: models, Index: index, SID: a.sid.Load(), Speed: fmt.Sprintf("%.02f", float64(a.speedX100.Load())/100), StartupText: a.cfg.StartupText}); err != nil {
		log.Printf("render index: %v", err)
	}
}

const maxSID = 904
const maxTextBytes = 256 << 10
const maxSentenceRunes = 1000

func validSpeed(speed float64) bool {
	return !math.IsNaN(speed) && !math.IsInf(speed, 0) && speed >= 0.2 && speed <= 4
}

func (a *App) handleSetSID(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		http.Error(w, "invalid form", 400)
		return
	}
	v, err := strconv.ParseInt(r.PostForm.Get("sid"), 10, 64)
	a.mu.Lock()
	defer a.mu.Unlock()
	if err != nil || v < 0 || v > maxSID || (a.speakerCount > 0 && v >= int64(a.speakerCount)) {
		http.Error(w, "invalid speaker id for selected model", 400)
		return
	}
	a.sid.Store(v)
}

func (a *App) handleSetSpeed(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		http.Error(w, "invalid form", 400)
		return
	}
	v, err := strconv.ParseFloat(r.PostForm.Get("speed"), 64)
	if err != nil || !validSpeed(v) {
		http.Error(w, "speed must be between 0.2 and 4", 400)
		return
	}
	a.speedX100.Store(int64(math.Round(v * 100)))
}

func (a *App) handleStop(w http.ResponseWriter, r *http.Request) {
	a.mu.Lock()
	a.stopLocked()
	a.mu.Unlock()
}

func (a *App) HandlePause(w http.ResponseWriter, r *http.Request) {
	a.mu.Lock()
	if a.progress.Playing {
		a.paused = !a.paused
		a.progress.Paused = a.paused
		if a.paused && a.pauseCancel != nil {
			a.pauseCancel()
		}
		a.cond.Broadcast()
	}
	paused := a.paused
	a.mu.Unlock()
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]bool{"paused": paused})
}

func (a *App) handlePlay(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		http.Error(w, "invalid form", 400)
		return
	}
	original := r.PostForm.Get("text")
	if len(original) > maxTextBytes {
		http.Error(w, "text too large", http.StatusRequestEntityTooLarge)
		return
	}
	if !utf8.ValidString(original) {
		http.Error(w, "invalid UTF-8 text", 400)
		return
	}
	text := removeEmptyLines(removeChars(original, r.PostForm.Get("exclude")), true)
	if strings.TrimSpace(text) == "" {
		http.Error(w, "empty text", 400)
		return
	}
	splitChars := r.PostForm.Get("split")
	if len(splitChars) > 128 || !utf8.ValidString(splitChars) {
		http.Error(w, "invalid sentence delimiters", 400)
		return
	}
	sentences := a.sentence.SplitWithDelimiters(text, splitChars)
	// Bound individual native generation calls even for input without punctuation.
	var chunks []string
	for _, sentence := range sentences {
		runes := []rune(sentence)
		for len(runes) > maxSentenceRunes {
			cut := maxSentenceRunes
			for j := cut; j > cut/2; j-- {
				if runes[j] == ' ' {
					cut = j
					break
				}
			}
			chunks = append(chunks, string(runes[:cut]))
			runes = runes[cut:]
		}
		if s := strings.TrimSpace(string(runes)); s != "" {
			chunks = append(chunks, s)
		}
	}
	if len(chunks) == 0 {
		http.Error(w, "empty text", 400)
		return
	}
	if !a.control.TryLock() {
		http.Error(w, "playback is being configured; retry shortly", http.StatusConflict)
		return
	}
	defer a.control.Unlock()
	a.mu.Lock()
	index := a.modelIndex
	name := r.PostForm.Get("model_name")
	if name != "" {
		index = a.cfg.ModelIndex(name)
	}
	if index < 0 {
		a.mu.Unlock()
		http.Error(w, "unknown model", 400)
		return
	}
	if a.closed {
		a.mu.Unlock()
		http.Error(w, "application is shutting down", 503)
		return
	}
	a.stopLocked()
	id := a.sessionID
	done := a.done
	a.mu.Unlock()
	if done != nil {
		select {
		case <-done:
		case <-r.Context().Done():
			return
		}
	}
	if a.engine == nil || index != a.modelIndex {
		engine, err := a.newEngine(a.cfg.Models[index])
		if err != nil {
			log.Printf("TTS initialization failed: %v", err)
			http.Error(w, "TTS initialization failed; check server logs", 500)
			return
		}
		if a.engine != nil {
			a.engine.Close()
		}
		a.engine = engine
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	a.modelIndex = index
	a.speakerCount = a.engine.NumSpeakers()
	if a.speakerCount <= 0 || a.sid.Load() >= int64(a.speakerCount) {
		http.Error(w, "speaker id unavailable for selected model", 400)
		return
	}
	// Stop may have arrived while loading a model or waiting on synthesis.
	if a.sessionID != id || r.Context().Err() != nil {
		http.Error(w, "play request canceled", 409)
		return
	}
	ctx, cancel := context.WithCancel(context.Background())
	a.cancel = cancel
	a.done = make(chan struct{})
	a.progress = ProgressResponse{Total: int64(len(chunks)), Playing: true}
	go a.playback(ctx, id, chunks, a.engine, a.done)
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(PlayResponse{Text: original})
}

type PlayResponse struct {
	Text string `json:"text"`
}

func removeChars(s, chars string) string {
	excluded := make(map[rune]bool)
	for _, r := range chars {
		excluded[r] = true
	}
	return strings.Map(func(r rune) rune {
		if excluded[r] {
			return -1
		}
		return r
	}, s)
}

func removeEmptyLines(s string, join bool) string {
	var b strings.Builder
	b.Grow(len(s))

	start := 0
	wrote := false

	sep := byte('\n')
	if join {
		sep = ' '
	}

	for i := 0; i <= len(s); i++ {
		if i < len(s) && s[i] != '\n' {
			continue
		}

		line := s[start:i]

		left, right := 0, len(line)
		for left < right {
			c := line[left]
			if c != ' ' && c != '\t' && c != '\r' {
				break
			}
			left++
		}
		for left < right {
			c := line[right-1]
			if c != ' ' && c != '\t' && c != '\r' {
				break
			}
			right--
		}

		if left < right {
			if wrote {
				b.WriteByte(sep)
			}
			b.WriteString(line)
			wrote = true
		}

		start = i + 1
	}

	return b.String()
}
