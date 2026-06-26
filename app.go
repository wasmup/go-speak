package main

import (
	"context"
	"fmt"
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
	playing     bool
	paused      bool
	cond        *sync.Cond
	cancel      context.CancelFunc
	pauseCancel context.CancelFunc

	// progress state
	progressDone    atomic.Int64
	progressTotal   atomic.Int64
	progressPlaying atomic.Bool

	playSessionID atomic.Int64

	currentSentence atomic.Value // string
}

func NewApp(cfg *Config) (*App, error) {
	a := &App{
		cfg: cfg,
	}
	a.cond = sync.NewCond(&a.mu)

	a.sid.Store(int64(cfg.SID))
	a.speedX100.Store(int64(cfg.Speed * 100))
	a.currentSentence.Store("")

	return a, nil
}

func (a *App) Playback(ctx context.Context, sessionID int64, sentences []string) {
	defer func() {
		a.mu.Lock()
		a.playing = false
		a.paused = false
		a.mu.Unlock()
		if a.playSessionID.Load() == sessionID {
			a.progressPlaying.Store(false)
			a.currentSentence.Store(``)
		}
	}()

	for i, s := range sentences {
		if ctx.Err() != nil || a.playSessionID.Load() != sessionID {
			fmt.Println(sessionID, i, `sessionID`, a.playSessionID.Load())
			return
		}

		a.currentSentence.Store(s)

		sid := int(a.sid.Load())
		speed := float32(a.speedX100.Load()) / 100

		a.ttsMu.Lock()
		audio := a.tts.Generate(s, sid, speed)
		a.ttsMu.Unlock()
		for { // pause resume
			if ctx.Err() != nil || a.playSessionID.Load() != sessionID {
				fmt.Println(sessionID, i, `sessionID`, a.playSessionID.Load())
				return
			}

			sentCtx, sentCancel := context.WithCancel(ctx)
			a.mu.Lock()
			a.playing = true
			a.pauseCancel = sentCancel // Store it so HandlePause can call it
			for a.paused && ctx.Err() == nil {
				a.cond.Wait() // Block here if paused
				fmt.Println(sessionID, i, `awakened`)
			}
			a.mu.Unlock()

			if ctx.Err() != nil || a.playSessionID.Load() != sessionID {
				fmt.Println(sessionID, i, `sessionID`, a.playSessionID.Load())
				return
			}
			fmt.Println(sessionID, i, s)
			err := play(sentCtx, audio)
			sentCancel() // Cleanup
			if err != nil {
				a.mu.Lock()
				paused := a.paused
				a.mu.Unlock()
				if paused {
					fmt.Println(sessionID, i, `paused:`, err)
					continue
				} else {
					fmt.Println(sessionID, i, `stopped:`, err)
				}
			}
			if a.playSessionID.Load() != sessionID {
				fmt.Println(sessionID, i, `sessionID`, a.playSessionID.Load())
				return
			}
			break
		}
		a.progressDone.Store(int64(i + 1))
	}

	a.currentSentence.Store("")
}
