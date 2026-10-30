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
