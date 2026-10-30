package main

import (
	"context"
	_ "embed"
	"encoding/base64"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"io"
	"math"
	"os"
	"os/exec"
	"path/filepath"
	"time"

	sherpa "github.com/k2-fsa/sherpa-onnx-go-linux"
)

const qwenModelName = "Qwen3-TTS-12Hz-1.7B-CustomVoice"

func localQwenPath(name, fallback string) string {
	candidates := []string{filepath.Join("build", name)}
	if executable, err := os.Executable(); err == nil {
		candidates = append(candidates, filepath.Join(filepath.Dir(executable), name))
	}
	for _, path := range candidates {
		check := path
		if name == "qwen-model" {
			check = filepath.Join(path, "config.json")
		}
		if _, err := os.Stat(check); err == nil {
			absolute, err := filepath.Abs(path)
			if err == nil {
				return absolute
			}
		}
	}
	return fallback
}

//go:embed qwen_worker.py
var qwenWorker string

// Requests are serialized by the app's playback worker. A canceled request kills
// the process; the next request starts a fresh worker so responses cannot cross.
type qwenEngine struct {
	cfg    *Config
	cmd    *exec.Cmd
	input  io.WriteCloser
	output *json.Decoder
}

type qwenResponse struct {
	SampleRate int    `json:"sample_rate"`
	Audio      string `json:"audio"`
	Error      string `json:"error"`
}

func newQwenEngine(cfg *Config) (*qwenEngine, error) {
	if _, err := exec.LookPath(cfg.QwenPython); err != nil {
		return nil, fmt.Errorf("Qwen Python unavailable: %w; see README Qwen setup", err)
	}
	return &qwenEngine{cfg: cfg}, nil
}

func (e *qwenEngine) NumSpeakers() int { return 9 }
func (e *qwenEngine) Close() {
	if e.cmd != nil {
		_ = e.input.Close()
		_ = e.cmd.Process.Kill()
		_ = e.cmd.Wait()
		e.cmd = nil
	}
}
func (e *qwenEngine) start() error {
	cmd := exec.Command(e.cfg.QwenPython, "-u", "-c", qwenWorker, e.cfg.QwenModel, e.cfg.QwenDevice)
	cmd.Stderr = os.Stderr
	input, err := cmd.StdinPipe()
	if err != nil {
		return err
	}
	output, err := cmd.StdoutPipe()
	if err != nil {
		_ = input.Close()
		return err
	}
	if err = cmd.Start(); err != nil {
		_ = input.Close()
		_ = output.Close()
		return err
	}
	e.cmd, e.input, e.output = cmd, input, json.NewDecoder(output)
	return nil
}
func (e *qwenEngine) Generate(text string, sid int, speed float32) *sherpa.GeneratedAudio {
	audio, _ := e.GenerateContext(context.Background(), text, sid, speed)
	return audio
}
func (e *qwenEngine) GenerateContext(ctx context.Context, text string, sid int, speed float32) (*sherpa.GeneratedAudio, error) {
	if sid < 0 || sid >= e.NumSpeakers() || !validSpeed(float64(speed)) {
		return nil, fmt.Errorf("invalid Qwen voice or speed")
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if e.cmd == nil {
		if err := e.start(); err != nil {
			return nil, err
		}
	}
	ctx, cancel := context.WithTimeout(ctx, 20*time.Minute)
	defer cancel()
	type result struct {
		response qwenResponse
		err      error
	}
	done := make(chan result, 1)
	go func() {
		err := json.NewEncoder(e.input).Encode(map[string]any{"text": text, "sid": sid, "speed": speed})
		var response qwenResponse
		if err == nil {
			err = e.output.Decode(&response)
		}
		done <- result{response, err}
	}()
	var r result
	select {
	case r = <-done:
	case <-ctx.Done():
		e.Close()
		<-done
		return nil, ctx.Err()
	}
	if r.err != nil {
		e.Close()
		return nil, fmt.Errorf("Qwen worker: %w (check server logs and Python dependencies)", r.err)
	}
	if r.response.Error != "" {
		return nil, fmt.Errorf("Qwen: %s", r.response.Error)
	}
	return decodeQwenAudio(r.response)
}

func decodeQwenAudio(r qwenResponse) (*sherpa.GeneratedAudio, error) {
	pcm, err := base64.StdEncoding.DecodeString(r.Audio)
	if err != nil || len(pcm) == 0 || len(pcm)%4 != 0 || r.SampleRate < 8000 || r.SampleRate > 192000 {
		return nil, fmt.Errorf("invalid Qwen audio response")
	}
	samples := make([]float32, len(pcm)/4)
	for i := range samples {
		samples[i] = math.Float32frombits(binary.LittleEndian.Uint32(pcm[i*4:]))
		if math.IsNaN(float64(samples[i])) || math.IsInf(float64(samples[i]), 0) {
			return nil, fmt.Errorf("non-finite Qwen audio")
		}
	}
	return &sherpa.GeneratedAudio{Samples: samples, SampleRate: r.SampleRate}, nil
}
