package main

import (
	"errors"
	"flag"
	"fmt"
	"io"
	"net"
	"os"
	"path/filepath"
	"strings"
	"unicode/utf8"
)

// Config holds server configuration.
type Config struct {
	Addr        string  // HTTP listen address
	Input       string  // optional startup text file
	Model       string  // TTS model directory
	SID         int64   // default speaker id
	Speed       float64 // default speech speed
	Player      string  // auto, pw-play, or aplay
	LLMClean    bool    // explicitly enable the optional local LLM cleaner
	StartupText string  // loaded startup text

	Models []Model // TTS models
	Name   string  // selected model name
	Index  int     // selected model index
}

func (cfg *Config) ModelIndex(modelName string) int {
	for i := range cfg.Models {
		if cfg.Models[i].Name == modelName {
			return i
		}
	}
	return -1
}

type Model struct {
	Name      string
	ModelFile string
}

// ParseFlags parses command-line flags and prepares the configuration.
func ParseFlags() (*Config, error) {
	return parseFlags(os.Args[1:])
}

func parseFlags(args []string) (*Config, error) {
	cfg := &Config{}
	flags := flag.NewFlagSet("go-speak", flag.ContinueOnError)

	flags.StringVar(&cfg.Addr, "addr", "127.0.0.1:8080", "web server listen address")
	flags.StringVar(&cfg.Input, "i", "", "startup text file (optional)")
	flags.StringVar(&cfg.Model, "m", "/opt/go-speak", "TTS model directory")
	flags.StringVar(&cfg.Name, "name", "sherpa-onnx-supertonic-3-tts-int8-2026-05-11", "pre selected TTS model name")

	flags.Int64Var(&cfg.SID, "sid", 4, "default speaker id")
	flags.Float64Var(&cfg.Speed, "speed", 1.1, "default speech speed")

	flags.StringVar(&cfg.Player, "player", "auto", "audio player: auto, pw-play, or aplay")
	flags.BoolVar(&cfg.LLMClean, "llm-clean", false, "enable optional text cleaning via localhost:8011")

	if err := flags.Parse(args); err != nil {
		return nil, err
	}
	if flags.NArg() != 0 {
		return nil, errors.New("unexpected positional arguments")
	}

	if cfg.Player != "auto" && cfg.Player != "aplay" && cfg.Player != "pw-play" {
		return nil, errors.New("player must be auto, pw-play, or aplay")
	}
	if !validSpeed(cfg.Speed) {
		return nil, errors.New("speed must be between 0.2 and 4")
	}
	if cfg.SID < 0 || cfg.SID > maxSID {
		return nil, errors.New("speaker id out of range")
	}
	host, _, err := net.SplitHostPort(cfg.Addr)
	if err != nil || net.ParseIP(host) == nil || !net.ParseIP(host).IsLoopback() {
		return nil, errors.New("listen address must use a loopback IP, for example 127.0.0.1:8080")
	}

	home, err := os.UserHomeDir()
	if err != nil {
		return nil, err
	}
	cfg.Model = expandHome(cfg.Model, home)
	cfg.Input = expandHome(cfg.Input, home)

	if cfg.Model == "" {
		return nil, errors.New("model directory must not be empty")
	}
	if _, err := os.Stat(cfg.Model); err != nil {
		return nil, fmt.Errorf("model directory error: %w", err)
	}

	if cfg.Input != "" {
		f, err := os.Open(cfg.Input)
		if err != nil {
			return nil, fmt.Errorf("input file error: %w", err)
		}
		defer f.Close()
		b, err := io.ReadAll(io.LimitReader(f, maxTextBytes+1))
		if err != nil {
			return nil, fmt.Errorf("input file error: %w", err)
		}
		if len(b) > maxTextBytes {
			return nil, errors.New("startup text exceeds 256 KiB")
		}
		if !utf8.Valid(b) {
			return nil, errors.New("startup text must be valid UTF-8")
		}
		cfg.StartupText = string(b)
	}

	files, err := ModelFiles(cfg.Model)
	if err != nil {
		return nil, err
	}
	m := map[string]bool{}
	for _, f := range files {
		name := filepath.Base(filepath.Dir(f))
		if m[name] {
			continue
		}
		cfg.Models = append(cfg.Models, Model{
			ModelFile: f,
			Name:      name,
		})
		m[name] = true
	}

	if len(cfg.Models) == 0 {
		return nil, errors.New("no ONNX models found in model directory")
	}
	cfg.Index = 0
	if cfg.Name != "" {
		cfg.Index = cfg.ModelIndex(cfg.Name)
		if cfg.Index < 0 {
			return nil, fmt.Errorf("unknown model %q", cfg.Name)
		}
	}

	return cfg, nil
}

// expandHome expands ~ and ~/path to the user's home directory,
// then expands environment variables.
func expandHome(p, home string) string {
	if p == "" {
		return p
	}

	if p == "~" {
		p = home
	} else if strings.HasPrefix(p, "~/") {
		p = filepath.Join(home, p[2:])
	}

	return os.ExpandEnv(p)
}
