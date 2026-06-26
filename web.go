package main

import (
	"context"
	"encoding/json"
	"fmt"
	"html/template"
	"net/http"
	"strconv"
	"strings"

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
	a.paused = false
	a.cond.Broadcast()
	a.mu.Unlock()

	// Invalidate currently running playback session.
	a.playSessionID.Add(1)
	a.progressPlaying.Store(false)
	a.currentSentence.Store("")
}

// func (a *App) HandlePause(w http.ResponseWriter, r *http.Request) {
// 	a.mu.Lock()
// 	if a.playing {
// 		a.paused = !a.paused
// 		if !a.paused {
// 			a.cond.Broadcast() // Resume loop
// 		}
// 	}
// 	paused := a.paused
// 	a.mu.Unlock()
// 	json.NewEncoder(w).Encode(map[string]bool{"paused": paused})
// }

func (a *App) HandlePause(w http.ResponseWriter, r *http.Request) {
	a.mu.Lock()
	if a.playing {
		a.paused = !a.paused
		if a.paused {
			if a.pauseCancel != nil {
				a.pauseCancel()
			}
		} else {
			a.cond.Broadcast()
		}
	}
	paused := a.paused
	a.mu.Unlock()
	json.NewEncoder(w).Encode(map[string]bool{"paused": paused})
}

func (a *App) handlePlay(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		http.Error(w, "invalid form", http.StatusBadRequest)
		return
	}

	splitChars := r.FormValue("split")
	if splitChars == "" {
		splitChars = `.!?:"“”`
	}

	text := r.FormValue("text")
	text = removeEmptyLines(text, true)
	if text == "" {
		http.Error(w, "empty text", 400)
		return
	}

	exclude := r.FormValue("exclude")
	if exclude != "" {
		text = removeChars(text, exclude)
	}

	sentences := splitSentences(text, splitChars)
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

	go a.Playback(ctx, sessionID, sentences)

	w.Header().Set("Content-Type", "application/json")

	json.NewEncoder(w).Encode(PlayResponse{
		Text: text, // strings.Join(sentences, "\n"),
	})
}

type PlayResponse struct {
	Text string `json:"text"`
}

func removeChars(s, chars string) string {
	if chars == "" {
		return s
	}

	var table [256]bool
	for i := 0; i < len(chars); i++ {
		table[chars[i]] = true
	}

	b := make([]byte, 0, len(s))
	for i := 0; i < len(s); i++ {
		if !table[s[i]] {
			b = append(b, s[i])
		}
	}

	return string(b)
}

// func removeEmptyLines(s string) string {
// 	var b strings.Builder
// 	b.Grow(len(s))

// 	start := 0
// 	wrote := false

// 	for i := 0; i <= len(s); i++ {
// 		if i < len(s) && s[i] != '\n' {
// 			continue
// 		}

// 		line := s[start:i]

// 		// trim ASCII space cheaply
// 		left, right := 0, len(line)
// 		for left < right {
// 			c := line[left]
// 			if c != ' ' && c != '\t' && c != '\r' {
// 				break
// 			}
// 			left++
// 		}
// 		for left < right {
// 			c := line[right-1]
// 			if c != ' ' && c != '\t' && c != '\r' {
// 				break
// 			}
// 			right--
// 		}

// 		if left < right {
// 			if wrote {
// 				b.WriteByte('\n')
// 			}
// 			b.WriteString(line)
// 			wrote = true
// 		}

// 		start = i + 1
// 	}

// 	return b.String()
// }

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
