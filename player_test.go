package main

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	sherpa "github.com/k2-fsa/sherpa-onnx-go-linux"
)

func TestPlayerIncludesDeviceError(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "aplay"), []byte("#!/bin/sh\necho 'audio open error: Device or resource busy' >&2\nexit 1\n"), 0755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", dir)
	err := playWithPlayer(context.Background(), &sherpa.GeneratedAudio{SampleRate: 24000, Samples: make([]float32, 24000)}, "aplay")
	if err == nil || !strings.Contains(err.Error(), "Device or resource busy") {
		t.Fatalf("missing audio device error: %v", err)
	}
}

func TestDefaultAudioDeviceRepeatedPlayback(t *testing.T) {
	if os.Getenv("GO_SPEAK_TEST_AUDIO_DEVICE") == "" {
		t.Skip("set GO_SPEAK_TEST_AUDIO_DEVICE to test silent playback on the default audio device")
	}
	player, err := resolvePlayer("auto")
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("selected player: %s", player)
	for i := 0; i < 4; i++ {
		if err := play(context.Background(), &sherpa.GeneratedAudio{SampleRate: 24000, Samples: make([]float32, 2400)}); err != nil {
			t.Fatalf("playback %d: %v", i+1, err)
		}
	}
}

func TestPlayerDeadlineReapsStalledProcess(t *testing.T) {
	// A player that never exits must be killed and reaped by the deadline.
	sleep, err := exec.LookPath("sleep")
	if err != nil {
		t.Fatal(err)
	}
	start := time.Now()
	err = runPlayer(context.Background(), sleep, []string{"30"}, &sherpa.GeneratedAudio{SampleRate: 24000, Samples: []float32{0}}, 50*time.Millisecond)
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("expected timeout, got %v", err)
	}
	if time.Since(start) > 2*time.Second {
		t.Fatal("stalled player was not reaped promptly")
	}
}

func TestPlayerCancellationRemainsCancellation(t *testing.T) {
	sleep, err := exec.LookPath("sleep")
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	timer := time.AfterFunc(50*time.Millisecond, cancel)
	defer timer.Stop()
	defer cancel()
	err = runPlayer(ctx, sleep, []string{"30"}, &sherpa.GeneratedAudio{SampleRate: 24000, Samples: []float32{0}}, time.Second)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("expected cancellation, got %v", err)
	}
}
