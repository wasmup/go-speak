package main

import (
	"context"
	"errors"
	"log"
	"math"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	sherpa "github.com/k2-fsa/sherpa-onnx-go-linux"
)

type speechEngine interface {
	Generate(string, int, float32) *sherpa.GeneratedAudio
	NumSpeakers() int
	Close()
}

type App struct {
	cfg      *Config
	sentence *SentenceSplitter
	// control serializes model ownership and new playback requests. mu protects
	// all playback/progress state; no native generation or process wait holds mu.
	control      sync.Mutex
	mu           sync.Mutex
	cond         *sync.Cond
	sid          atomic.Int64
	speedX100    atomic.Int64
	engine       speechEngine
	modelIndex   int
	speakerCount int
	newEngine    func(Model) (speechEngine, error)
	playAudio    func(context.Context, *sherpa.GeneratedAudio) error
	cancel       context.CancelFunc
	pauseCancel  context.CancelFunc
	done         chan struct{}
	sessionID    int64
	paused       bool
	closed       bool
	progress     ProgressResponse
}

func NewApp(cfg *Config) (*App, error) {
	if cfg == nil || len(cfg.Models) == 0 || cfg.Index < 0 || cfg.Index >= len(cfg.Models) {
		return nil, errors.New("no valid TTS model selected")
	}
	if !validSpeed(cfg.Speed) || cfg.SID < 0 || cfg.SID > maxSID {
		return nil, errors.New("invalid playback defaults")
	}
	a := &App{cfg: cfg, sentence: NewSentenceSplitter(), modelIndex: cfg.Index,
		newEngine: func(m Model) (speechEngine, error) {
			tts, err := NewTTS(m)
			if err != nil {
				return nil, err
			}
			return &nativeEngine{tts}, nil
		}, playAudio: play}
	a.cond = sync.NewCond(&a.mu)
	a.sid.Store(cfg.SID)
	a.speedX100.Store(int64(math.Round(cfg.Speed * 100)))
	return a, nil
}

// stopLocked invalidates the worker before publishing idle state. A native
// Generate call cannot be interrupted; the next session waits for it to return.
func (a *App) stopLocked() {
	if a.cancel != nil {
		a.cancel()
		a.cancel = nil
	}
	a.sessionID++
	a.paused = false
	a.pauseCancel = nil
	a.progress.Playing = false
	a.progress.Paused = false
	a.progress.Current = ""
	a.cond.Broadcast()
}

func (a *App) Close() {
	a.control.Lock()
	defer a.control.Unlock()
	a.mu.Lock()
	a.closed = true
	a.stopLocked()
	done := a.done
	a.mu.Unlock()
	if done != nil {
		<-done
	}
	if a.engine != nil {
		a.engine.Close()
		a.engine = nil
	}
}

func (a *App) playback(ctx context.Context, id int64, sentences []string, engine speechEngine, done chan struct{}) {
	defer close(done)
	defer func() {
		a.mu.Lock()
		defer a.mu.Unlock()
		if a.sessionID == id {
			a.progress.Playing = false
			a.progress.Paused = false
			a.progress.Current = ""
			a.paused = false
			a.pauseCancel = nil
		}
	}()
	fail := func(message string) {
		log.Print(message)
		a.mu.Lock()
		if a.sessionID == id {
			a.progress.Error = message
		}
		a.mu.Unlock()
	}
	for i, sentence := range sentences {
		if ctx.Err() != nil {
			return
		}
		if a.cfg.LLMClean && needsLLMClean(sentence) {
			if cleaned, err := Clean(ctx, sentence); err == nil {
				sentence = cleaned
			} else if ctx.Err() == nil {
				log.Printf("text cleaning failed: %v", err)
			}
		}
		a.mu.Lock()
		for a.paused && ctx.Err() == nil {
			a.cond.Wait()
		}
		if ctx.Err() != nil || a.sessionID != id {
			a.mu.Unlock()
			return
		}
		a.progress.Current = sentence
		a.mu.Unlock()
		audio := engine.Generate(sentence, int(a.sid.Load()), float32(a.speedX100.Load())/100)
		if ctx.Err() != nil {
			return
		}
		if audio == nil || audio.SampleRate <= 0 || len(audio.Samples) == 0 {
			fail("speech synthesis returned no audio")
			return
		}
		var duration time.Duration
		for {
			a.mu.Lock()
			for a.paused && ctx.Err() == nil {
				a.cond.Wait()
			}
			if ctx.Err() != nil || a.sessionID != id {
				a.mu.Unlock()
				return
			}
			sentCtx, sentCancel := context.WithCancel(ctx)
			a.pauseCancel = sentCancel
			a.mu.Unlock()
			start := time.Now()
			err := a.playAudio(sentCtx, audio)
			duration += time.Since(start)
			interrupted := sentCtx.Err() != nil
			sentCancel()
			a.mu.Lock()
			a.pauseCancel = nil
			a.mu.Unlock()
			if ctx.Err() != nil {
				return
			}
			// A quick pause/resume still needs to replay the interrupted sentence.
			if interrupted {
				continue
			}
			if err != nil {
				fail("audio playback failed: " + err.Error())
				return
			}
			break
		}
		a.mu.Lock()
		if a.sessionID == id {
			a.progress.Done = int64(i + 1)
			if duration > 0 {
				a.progress.WPM = int(float64(len(strings.Fields(sentence))) / duration.Minutes())
			}
		}
		a.mu.Unlock()
	}
}
