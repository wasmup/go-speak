package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestConfigurationValidation(t *testing.T) {
	root := t.TempDir()
	for _, args := range [][]string{
		{"-m", root}, {"-m", root, "-speed", "NaN"}, {"-m", root, "-sid", "-1"},
		{"-m", root, "-addr", "0.0.0.0:8080"}, {"-m", root, "-name", "missing"},
	} {
		if _, err := parseFlags(args); err == nil {
			t.Fatalf("accepted %v", args)
		}
	}
	dir := filepath.Join(root, "vits-test")
	if err := os.Mkdir(dir, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "model.onnx"), []byte("placeholder"), 0644); err != nil {
		t.Fatal(err)
	}
	cfg, err := parseFlags([]string{"-m", root})
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Index != 0 || cfg.LLMClean || cfg.Speed != 1 {
		t.Fatalf("unexpected defaults: %+v", cfg)
	}
	if _, err := parseFlags([]string{"-m", root, "-name", "missing"}); err == nil {
		t.Fatal("unknown model accepted")
	}
	if _, err := NewTTS(cfg.Models[0]); err == nil || !strings.Contains(err.Error(), "tokens.txt") {
		t.Fatalf("missing native assets: %v", err)
	}
}

func TestModelDiscoveryRejectsAmbiguousGraphs(t *testing.T) {
	root := t.TempDir()
	dir := filepath.Join(root, "vits-test")
	if err := os.Mkdir(dir, 0755); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"one.onnx", "two.onnx"} {
		if err := os.WriteFile(filepath.Join(dir, name), []byte("placeholder"), 0644); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := ModelFiles(root); err == nil {
		t.Fatal("ambiguous model accepted")
	}
}

func TestKokoroDiscoveryIgnoresAuxiliaryGraphs(t *testing.T) {
	root := t.TempDir()
	dir := filepath.Join(root, "kokoro-en-v0_19")
	if err := os.Mkdir(dir, 0755); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"model.onnx", "vocos-22khz-univ.onnx"} {
		if err := os.WriteFile(filepath.Join(dir, name), []byte("placeholder"), 0644); err != nil {
			t.Fatal(err)
		}
	}
	cfg, err := parseFlags([]string{"-m", root, "-name", "kokoro-en-v0_19"})
	if err != nil {
		t.Fatal(err)
	}
	if len(cfg.Models) != 1 || cfg.Models[0].ModelFile != filepath.Join(dir, "model.onnx") {
		t.Fatalf("unexpected models: %+v", cfg.Models)
	}
}

// Opt in with a real model path; normal CI does not need model downloads.
func TestKokoroNativeSynthesis(t *testing.T) {
	root := os.Getenv("GO_SPEAK_KOKORO_MODEL")
	if root == "" {
		t.Skip("set GO_SPEAK_KOKORO_MODEL to a Kokoro model directory")
	}
	cfg, err := parseFlags([]string{"-m", filepath.Dir(root), "-name", filepath.Base(root)})
	if err != nil {
		t.Fatal(err)
	}
	engine, err := NewTTS(cfg.Models[cfg.Index])
	if err != nil {
		t.Fatal(err)
	}
	defer (&nativeEngine{engine}).Close()
	if engine.NumSpeakers() <= 0 {
		t.Fatal("model has no speakers")
	}
	audio := engine.Generate("Hello, this is a speech test.", 0, 1)
	if audio == nil || audio.SampleRate <= 0 || len(audio.Samples) == 0 {
		t.Fatal("no audio generated")
	}
	t.Logf("generated %d samples at %d Hz with %d speakers", len(audio.Samples), audio.SampleRate, engine.NumSpeakers())
}
