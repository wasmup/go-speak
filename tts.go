package main

import (
	"errors"
	"path/filepath"

	sherpa "github.com/k2-fsa/sherpa-onnx-go-linux"
)

const (
	modelSubdir = "vits-piper-en_US-libritts_r-medium"
	modelFile   = "en_US-libritts_r-medium.onnx"
	tokensFile  = "tokens.txt"
	espeakDir   = "espeak-ng-data"
)

func NewTTS(dir string) (*sherpa.OfflineTts, error) {
	modelRoot := filepath.Join(dir, modelSubdir)

	ttsCfg := sherpa.OfflineTtsConfig{
		Model: sherpa.OfflineTtsModelConfig{
			Vits: sherpa.OfflineTtsVitsModelConfig{
				Model:   filepath.Join(modelRoot, modelFile),
				Tokens:  filepath.Join(modelRoot, tokensFile),
				DataDir: filepath.Join(modelRoot, espeakDir),
			},
			NumThreads: 4,
		},
	}

	tts := sherpa.NewOfflineTts(&ttsCfg)
	if tts == nil {
		return nil, errors.New("failed to initialize TTS")
	}

	return tts, nil
}
