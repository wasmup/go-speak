package main

import (
	"errors"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// Config holds server configuration.
type Config struct {
	Addr        string  // HTTP listen address
	Input       string  // optional startup text file
	Model       string  // TTS model directory
	SID         int64   // default speaker id
	Speed       float64 // default speech speed
	StartupText string  // loaded startup text

	Models []Model // TTS models
	Index  int     // selected model file index
}

type Model struct {
	Name      string
	ModelFile string
}

// ParseFlags parses command-line flags and prepares the configuration.
func ParseFlags() (*Config, error) {
	cfg := &Config{}

	flag.StringVar(&cfg.Addr, "addr", "127.0.0.1:8080", "web server listen address")
	flag.StringVar(&cfg.Input, "i", "", "startup text file (optional)")
	flag.StringVar(&cfg.Model, "m", "/opt/go-speak", "TTS model directory")

	flag.Int64Var(&cfg.SID, "sid", 7, "default speaker id")
	flag.Float64Var(&cfg.Speed, "speed", 1.0, "default speech speed")

	flag.Parse()

	if cfg.Speed <= 0 {
		cfg.Speed = 1.0
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
		b, err := os.ReadFile(cfg.Input)
		if err != nil {
			return nil, fmt.Errorf("input file error: %w", err)
		}
		cfg.StartupText = string(b)
	}

	files, err := ModelFiles(cfg.Model)
	if err != nil {
		return nil, err
	}
	for _, f := range files {
		cfg.Models = append(cfg.Models, Model{
			ModelFile: f,
			Name:      filepath.Base(filepath.Dir(f)),
		})
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
