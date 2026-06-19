package main

import (
	"context"
	"sync"
	"sync/atomic"

	sherpa "github.com/k2-fsa/sherpa-onnx-go-linux"
)

type App struct {
	cfg *Config

	// TTS engine
	tts   *sherpa.OfflineTts
	ttsMu sync.Mutex

	mu        sync.Mutex
	sid       atomic.Int64
	speedX100 atomic.Int64

	// playback control
	cancel context.CancelFunc

	// progress state
	progressDone    atomic.Int64
	progressTotal   atomic.Int64
	progressPlaying atomic.Bool

	playSessionID atomic.Int64

	currentSentence atomic.Value // string
}

func NewApp(cfg *Config) (*App, error) {
	tts, err := NewTTS(cfg.Model)
	if err != nil {
		return nil, err
	}

	app := &App{
		cfg: cfg,
		tts: tts,
	}

	app.sid.Store(int64(cfg.SID))
	app.speedX100.Store(int64(cfg.Speed * 100))
	app.currentSentence.Store("")

	return app, nil
}

func (a *App) playLoop(ctx context.Context, sessionID int64, sentences []string) {
	defer func() {
		if a.playSessionID.Load() == sessionID {
			a.progressPlaying.Store(false)
			a.currentSentence.Store(``)
		}
	}()

	for i, s := range sentences {
		if ctx.Err() != nil || a.playSessionID.Load() != sessionID {
			return
		}

		a.currentSentence.Store(s)

		sid := int(a.sid.Load())
		speed := float32(a.speedX100.Load()) / 100

		a.ttsMu.Lock()
		audio := a.tts.Generate(s, sid, speed)
		a.ttsMu.Unlock()

		if ctx.Err() != nil || a.playSessionID.Load() != sessionID {
			return
		}

		if a.playSessionID.Load() != sessionID {
			return
		}

		if err := play(ctx, audio); err != nil {
			return
		}
		if a.playSessionID.Load() != sessionID {
			return
		}

		a.progressDone.Store(int64(i + 1))
	}

	a.currentSentence.Store("")
}
