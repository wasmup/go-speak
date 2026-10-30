package main

import (
	"context"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	sherpa "github.com/k2-fsa/sherpa-onnx-go-linux"
)

// GO_SPEAK_KOKORO_MODEL enables real native synthesis without playing sound.
func TestKokoroMultipleSentencePlayback(t *testing.T) {
	root := os.Getenv("GO_SPEAK_KOKORO_MODEL")
	if root == "" {
		t.Skip("set GO_SPEAK_KOKORO_MODEL to a Kokoro model directory")
	}
	cfg, err := parseFlags([]string{"-m", filepath.Dir(root), "-name", filepath.Base(root)})
	if err != nil {
		t.Fatal(err)
	}
	a, err := NewApp(cfg)
	if err != nil {
		t.Fatal(err)
	}
	defer a.Close()
	backend := "aplay"
	if os.Getenv("GO_SPEAK_TEST_AUDIO_DEVICE") != "" {
		backend, err = resolvePlayer("auto")
		if err != nil {
			t.Fatal(err)
		}
		if backend != "pw-play" {
			t.Skip("native PipeWire session unavailable")
		}
	}
	executable, err := exec.LookPath(backend)
	if err != nil {
		t.Fatal(err)
	}
	bin := t.TempDir()
	extra := "-D null"
	if backend == "pw-play" {
		extra = "--volume 0"
	}
	script := "#!/bin/sh\nexec '" + strings.ReplaceAll(executable, "'", "'\"'\"'") + "' " + extra + " \"$@\"\n"
	if err := os.WriteFile(filepath.Join(bin, backend), []byte(script), 0755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))
	cfg.Player = backend
	played := 0
	originalPlay := a.playAudio
	a.playAudio = func(ctx context.Context, audio *sherpa.GeneratedAudio) error {
		played++
		return originalPlay(ctx, audio)
	}
	for _, text := range []string{
		`Good developers’ caution about agentic development comes from knowing that built isn’t the same as working in production.

I’ve been surprised by how many excellent developers have been hesitant to go all in on coding agents. These are people who usually try new ways of working before anyone else and in many cases invent new tools and practices that support building the application well. So why have we had so much reluctance?`,
		"First sentence. Second sentence! Third sentence?",
		"First sentence. ... Second sentence! Third sentence?",
	} {
		played = 0
		w := post(a, "/play", url.Values{"text": {text}})
		if w.Code != 200 {
			t.Fatal(w.Body.String())
		}
		a.mu.Lock()
		done := a.done
		a.mu.Unlock()
		select {
		case <-done:
		case <-time.After(45 * time.Second):
			t.Fatal("playback did not finish")
		}
		a.mu.Lock()
		p := a.progress
		a.mu.Unlock()
		t.Logf("backend=%s played=%d progress=%+v", backend, played, p)
		if p.Error != "" || p.Done != p.Total || p.Total < 3 || played != int(p.Total) {
			t.Errorf("playback stopped early: %+v", p)
		}
	}
}
