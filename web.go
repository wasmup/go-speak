package main

import (
	"bytes"
	"context"
	"encoding/binary"
	"os/exec"

	_ "embed"
	"encoding/json"
	"fmt"
	sherpa "github.com/k2-fsa/sherpa-onnx-go-linux"
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
	a.stopLocked("stop_request")
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
	log.Printf("playback session=%d event=pause_request paused=%t playing=%t", a.sessionID, a.paused, a.progress.Playing)
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
	a.stopLocked("replacement_play_request")
	id := a.sessionID
	done := a.done
	a.mu.Unlock()
	traceCtx := withPlaybackTrace(r.Context(), id, 0, len(chunks))
	traceLog(traceCtx, "play_request", "model=%q text_bytes=%d chunks=%d sid=%d speed=%.2f player=%q", a.cfg.Models[index].Name, len(original), len(chunks), a.sid.Load(), float64(a.speedX100.Load())/100, a.cfg.Player)
	if done != nil {
		finishWait := traceStage(traceCtx, "previous_worker_wait", "")
		select {
		case <-done:
			finishWait("completed=true")
		case <-r.Context().Done():
			finishWait(fmt.Sprintf("error=%v", r.Context().Err()))
			return
		}
	}
	if a.engine == nil || index != a.modelIndex {
		finishLoad := traceStage(traceCtx, "model_load", fmt.Sprintf("name=%q path=%q", a.cfg.Models[index].Name, a.cfg.Models[index].ModelFile))
		engine, err := a.newEngine(a.cfg.Models[index])
		finishLoad(fmt.Sprintf("error=%v", err))
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
	a.modelIndex = index
	a.speakerCount = a.engine.NumSpeakers()
	if a.speakerCount <= 0 || a.sid.Load() >= int64(a.speakerCount) {
		a.mu.Unlock()
		http.Error(w, "speaker id unavailable for selected model", 400)
		return
	}
	// Stop may have arrived while loading a model or waiting on synthesis.
	if a.sessionID != id || r.Context().Err() != nil {
		a.mu.Unlock()
		http.Error(w, "play request canceled", 409)
		return
	}
	ctx, cancel := context.WithCancel(context.Background())
	a.cancel = cancel
	a.done = make(chan struct{})
	a.progress = ProgressResponse{Total: int64(len(chunks)), Playing: true}
	traceLog(traceCtx, "play_accepted", "speakers=%d", a.speakerCount)
	done = a.done
	saving := r.URL.Path == "/save_mp3"
	var pcm bytes.Buffer
	sampleRate := 0
	output := a.playAudio
	if saving {
		a.saveSound = r.PostForm.Get("play_sound") == "true"
		output = func(ctx context.Context, audio *sherpa.GeneratedAudio) error {
			if sampleRate == 0 {
				sampleRate = audio.SampleRate
			}
			if sampleRate != audio.SampleRate {
				return fmt.Errorf("audio sample rate changed during synthesis")
			}
			a.mu.Lock()
			sound := a.saveSound
			soundCtx, soundCancel := context.WithCancel(ctx)
			if sound {
				a.saveSoundCancel = soundCancel
			}
			a.mu.Unlock()
			if sound {
				err := a.playAudio(soundCtx, audio)
				a.mu.Lock()
				a.saveSoundCancel = nil
				a.mu.Unlock()
				if err != nil && soundCtx.Err() == nil {
					soundCancel()
					return err
				}
			}
			soundCancel()
			if err := ctx.Err(); err != nil {
				return err
			}
			for _, sample := range audio.Samples {
				if sample > 1 {
					sample = 1
				}
				if sample < -1 {
					sample = -1
				}
				if err := binary.Write(&pcm, binary.LittleEndian, int16(sample*32767)); err != nil {
					return err
				}
			}
			return nil
		}
	}
	go a.playback(ctx, id, chunks, a.engine, done, output)
	a.mu.Unlock()
	if saving {
		select {
		case <-done:
		case <-r.Context().Done():
			a.mu.Lock()
			if a.sessionID == id {
				a.stopLocked("export_disconnected")
			}
			a.mu.Unlock()
			return
		}
		a.mu.Lock()
		message, active := a.progress.Error, a.sessionID == id
		a.mu.Unlock()
		if !active {
			http.Error(w, "MP3 export canceled", http.StatusConflict)
			return
		}
		if message != "" {
			http.Error(w, message, http.StatusInternalServerError)
			return
		}
		cmd := exec.CommandContext(r.Context(), "ffmpeg", "-hide_banner", "-loglevel", "error", "-f", "s16le", "-ar", strconv.Itoa(sampleRate), "-ac", "1", "-i", "pipe:0", "-f", "mp3", "pipe:1")
		cmd.Stdin = &pcm
		var encoded, stderr bytes.Buffer
		cmd.Stdout, cmd.Stderr = &encoded, &stderr
		if err := cmd.Run(); err != nil {
			http.Error(w, "MP3 encoding failed (ffmpeg required): "+strings.TrimSpace(stderr.String())+" "+err.Error(), 500)
			return
		}
		w.Header().Set("Content-Type", "audio/mpeg")
		_, _ = w.Write(encoded.Bytes())
		return
	}
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

func (a *App) handleSaveSound(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		http.Error(w, "invalid form", http.StatusBadRequest)
		return
	}
	a.mu.Lock()
	a.saveSound = r.PostForm.Get("enabled") == "true"
	if !a.saveSound && a.saveSoundCancel != nil {
		a.saveSoundCancel()
	}
	a.mu.Unlock()
}
