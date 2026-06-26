package main

import (
	"errors"
	"os"
	"path/filepath"
	"strings"

	sherpa "github.com/k2-fsa/sherpa-onnx-go-linux"
)

const (
	tokensFile = "tokens.txt"
	espeakDir  = "espeak-ng-data"
)

func NewTTS(model Model) (*sherpa.OfflineTts, error) {
	modelRoot := filepath.Dir(model.ModelFile)

	ttsCfg := sherpa.OfflineTtsConfig{
		Model: sherpa.OfflineTtsModelConfig{
			Vits: sherpa.OfflineTtsVitsModelConfig{
				Model:   model.ModelFile,
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

// ModelFiles scans only the direct child directories of root.
// For each child directory, if it contains one or more ".onnx" files directly
// inside it, those files are returned as full paths.
//
// It does NOT recurse beyond one directory level.
func ModelFiles(root string) ([]string, error) {
	entries, err := os.ReadDir(root)
	if err != nil {
		return nil, err
	}

	var out []string

	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}

		dirPath := filepath.Join(root, entry.Name())

		childEntries, err := os.ReadDir(dirPath)
		if err != nil {
			return nil, err
		}

		for _, child := range childEntries {
			if child.IsDir() {
				continue
			}

			if strings.EqualFold(filepath.Ext(child.Name()), ".onnx") {
				fullPath := filepath.Join(dirPath, child.Name())

				// normalization to absolute path
				absPath, err := filepath.Abs(fullPath)
				if err != nil {
					return nil, err
				}

				out = append(out, absPath)
			}
		}
	}

	return out, nil
}
