package main

import (
	"context"
	"errors"
	"fmt"
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
	control         sync.Mutex
	mu              sync.Mutex
	cond            *sync.Cond
	sid             atomic.Int64
	speedX100       atomic.Int64
	engine          speechEngine
	modelIndex      int
	speakerCount    int
	newEngine       func(Model) (speechEngine, error)
	playAudio       func(context.Context, *sherpa.GeneratedAudio) error
	cancel          context.CancelFunc
	pauseCancel     context.CancelFunc
	saveSound       bool
	saveSoundCancel context.CancelFunc
	done            chan struct{}
	sessionID       int64
	paused          bool
	closed          bool
	progress        ProgressResponse
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
			if m.Name == qwenModelName {
				return newQwenEngine(cfg)
			}
			tts, err := NewTTS(m)
			if err != nil {
				return nil, err
			}
			return &nativeEngine{tts}, nil
		}, playAudio: func(ctx context.Context, audio *sherpa.GeneratedAudio) error {
			return playWithPlayer(ctx, audio, cfg.Player)
		}}
	a.cond = sync.NewCond(&a.mu)
	a.sid.Store(cfg.SID)
	a.speedX100.Store(int64(math.Round(cfg.Speed * 100)))
	return a, nil
}

// stopLocked invalidates the worker before publishing idle state. A native
// Generate call cannot be interrupted; the next session waits for it to return.
func (a *App) stopLocked(reason string) {
	log.Printf("playback session=%d event=cancel reason=%s playing=%t paused=%t done=%d/%d", a.sessionID, reason, a.progress.Playing, a.paused, a.progress.Done, a.progress.Total)
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
	a.stopLocked("close")
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

func (a *App) playback(ctx context.Context, id int64, sentences []string, engine speechEngine, done chan struct{}, output func(context.Context, *sherpa.GeneratedAudio) error) {
	ctx = withPlaybackTrace(ctx, id, 0, len(sentences))
	completed := 0
	outcome := "completed"
	traceLog(ctx, "session_start", "sentences=%d", len(sentences))
	defer close(done)
	defer func() {
		a.mu.Lock()
		defer a.mu.Unlock()
		if ctx.Err() != nil {
			outcome = "canceled"
		} else if a.sessionID != id {
			outcome = "superseded"
		}
		traceLog(ctx, "session_end", "outcome=%s done=%d/%d context_error=%v active_session=%d", outcome, completed, len(sentences), ctx.Err(), a.sessionID)
		if a.sessionID == id {
			a.progress.Playing = false
			a.progress.Paused = false
			a.progress.Current = ""
			a.paused = false
			a.pauseCancel = nil
		}
	}()
	fail := func(message string) {
		outcome = "error"
		traceLog(ctx, "session_error", "error=%q", message)
		a.mu.Lock()
		if a.sessionID == id {
			a.progress.Error = message
		}
		a.mu.Unlock()
	}
	for i, sentence := range sentences {
		ctx := withPlaybackTrace(ctx, id, i+1, len(sentences))
		if ctx.Err() != nil {
			return
		}
		if a.cfg.LLMClean && needsLLMClean(sentence) {
			finishClean := traceStage(ctx, "clean", "optional_llm=true")
			cleaned, err := Clean(ctx, sentence)
			finishClean(fmt.Sprintf("error=%v", err))
			if err == nil {
				sentence = cleaned
			} else if ctx.Err() == nil {
				log.Printf("text cleaning failed: %v", err)
			}
		}
		a.mu.Lock()
		for a.paused && ctx.Err() == nil {
			traceLog(ctx, "pause_wait", "paused=true")
			a.cond.Wait()
			traceLog(ctx, "pause_wake", "paused=%t context_error=%v", a.paused, ctx.Err())
		}
		if ctx.Err() != nil || a.sessionID != id {
			a.mu.Unlock()
			return
		}
		a.progress.Current = sentence
		a.mu.Unlock()
		sid, speed := int(a.sid.Load()), float32(a.speedX100.Load())/100
		finishGenerate := traceStage(ctx, "generate", fmt.Sprintf("text_bytes=%d words=%d sid=%d speed=%.2f", len(sentence), len(strings.Fields(sentence)), sid, speed))
		var audio *sherpa.GeneratedAudio
		var generateErr error
		if contextual, ok := engine.(interface {
			GenerateContext(context.Context, string, int, float32) (*sherpa.GeneratedAudio, error)
		}); ok {
			audio, generateErr = contextual.GenerateContext(ctx, sentence, sid, speed)
		} else {
			audio = engine.Generate(sentence, sid, speed)
		}
		result := "audio=nil"
		if audio != nil {
			result = fmt.Sprintf("samples=%d sample_rate=%d", len(audio.Samples), audio.SampleRate)
		}
		finishGenerate(result)
		if ctx.Err() != nil {
			return
		}
		if generateErr != nil {
			fail("speech synthesis failed: " + generateErr.Error())
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
				traceLog(ctx, "pause_wait", "paused=true")
				a.cond.Wait()
				traceLog(ctx, "pause_wake", "paused=%t context_error=%v", a.paused, ctx.Err())
			}
			if ctx.Err() != nil || a.sessionID != id {
				a.mu.Unlock()
				return
			}
			sentCtx, sentCancel := context.WithCancel(ctx)
			a.pauseCancel = sentCancel
			a.mu.Unlock()
			start := time.Now()
			finishAudio := traceStage(ctx, "audio", fmt.Sprintf("samples=%d sample_rate=%d", len(audio.Samples), audio.SampleRate))
			err := output(sentCtx, audio)
			finishAudio(fmt.Sprintf("error=%v context_error=%v", err, sentCtx.Err()))
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
				traceLog(ctx, "sentence_replay", "reason=interrupted")
				continue
			}
			if err != nil {
				fail("audio playback failed: " + err.Error())
				return
			}
			break
		}
		completed = i + 1
		traceLog(ctx, "sentence_complete", "done=%d/%d audio_elapsed=%s", completed, len(sentences), duration.Round(time.Millisecond))
		a.mu.Lock()
		if a.sessionID == id {
			a.progress.Done = int64(i + 1)
			a.progress.Words += len(strings.Fields(sentence))
			a.progress.Duration += float64(len(audio.Samples)) / float64(audio.SampleRate)
			if a.progress.Duration > 0 {
				a.progress.WPM = int(math.Round(float64(a.progress.Words) * 60 / a.progress.Duration))
			}
		}
		a.mu.Unlock()
	}
}
