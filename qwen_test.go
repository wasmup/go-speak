package main

import (
	"context"
	"encoding/base64"
	"encoding/binary"
	"errors"
	"math"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestQwenDefaultWithoutONNXModels(t *testing.T) {
	cfg, err := parseFlags([]string{"-m", filepath.Join(t.TempDir(), "missing"), "-name", qwenModelName})
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Models[cfg.Index].Name != qwenModelName || cfg.QwenDevice != "auto" {
		t.Fatalf("unexpected config: %+v", cfg)
	}
}

func TestDecodeQwenAudio(t *testing.T) {
	pcm := make([]byte, 8)
	binary.LittleEndian.PutUint32(pcm, math.Float32bits(0.25))
	binary.LittleEndian.PutUint32(pcm[4:], math.Float32bits(-0.5))
	encoded := base64.StdEncoding.EncodeToString(pcm)
	audio, err := decodeQwenAudio(qwenResponse{SampleRate: 24000, Audio: encoded})
	if err != nil || audio.Samples[0] != 0.25 || audio.Samples[1] != -0.5 {
		t.Fatalf("audio=%+v err=%v", audio, err)
	}
	for _, response := range []qwenResponse{{SampleRate: 0, Audio: encoded}, {SampleRate: 24000, Audio: "bad"}, {SampleRate: 24000, Audio: ""}} {
		if _, err := decodeQwenAudio(response); err == nil {
			t.Fatal("accepted invalid audio")
		}
	}
}

func TestQwenWorkerReuseAndCancellation(t *testing.T) {
	python, err := execPythonForTest()
	if err != nil {
		t.Skip(err)
	}
	original := qwenWorker
	defer func() { qwenWorker = original }()
	qwenWorker = "import sys, json, time\nfor line in sys.stdin:\n r=json.loads(line)\n if r['text']=='wait': time.sleep(30)\n print(json.dumps({'sample_rate':24000,'audio':'AACAPg=='}), flush=True)\n"
	engine, err := newQwenEngine(&Config{QwenPython: python})
	if err != nil {
		t.Fatal(err)
	}
	defer engine.Close()
	for range 2 {
		if _, err := engine.GenerateContext(context.Background(), "hello", 4, 1); err != nil {
			t.Fatal(err)
		}
	}
	pid := engine.cmd.Process.Pid
	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()
	if _, err := engine.GenerateContext(ctx, "wait", 4, 1); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("cancel: %v", err)
	}
	if engine.cmd != nil {
		t.Fatal("worker survived cancellation")
	}
	if _, err := engine.GenerateContext(context.Background(), "hello", 4, 1); err != nil {
		t.Fatal(err)
	}
	if engine.cmd.Process.Pid == pid {
		t.Fatal("worker did not restart")
	}
}

func execPythonForTest() (string, error) {
	for _, path := range []string{"/usr/bin/python3", "/usr/local/bin/python3"} {
		if _, err := os.Stat(path); err == nil {
			return path, nil
		}
	}
	return "", errors.New("Python unavailable")
}

// Opt in after setup-qwen.sh; normal tests never download or load model weights.
func TestQwenNativeSynthesis(t *testing.T) {
	if os.Getenv("GO_SPEAK_QWEN_TEST") != "1" {
		t.Skip("set GO_SPEAK_QWEN_TEST=1 after Qwen setup")
	}
	cfg, err := parseFlags([]string{"-name", qwenModelName})
	if err != nil {
		t.Fatal(err)
	}
	engine, err := newQwenEngine(cfg)
	if err != nil {
		t.Fatal(err)
	}
	defer engine.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Minute)
	defer cancel()
	start := time.Now()
	audio, err := engine.GenerateContext(ctx, "Hello, welcome back.", 4, 1)
	if err != nil {
		t.Fatal(err)
	}
	if len(audio.Samples) < audio.SampleRate/2 {
		t.Fatal("generated audio is too short")
	}
	t.Logf("generated %.2fs audio in %s at %d Hz", float64(len(audio.Samples))/float64(audio.SampleRate), time.Since(start).Round(time.Millisecond), audio.SampleRate)
}
