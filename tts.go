package main

import (
	"errors"
	"os"
	"path/filepath"
	"strings"

	sherpa "github.com/k2-fsa/sherpa-onnx-go-linux"
)

func NewTTS(model Model) (*sherpa.OfflineTts, error) {
	modelRoot := filepath.Dir(model.ModelFile)

	if strings.HasPrefix(model.Name, "sherpa-onnx-supertonic") {
		ttsCfg := sherpa.OfflineTtsConfig{
			Model: sherpa.OfflineTtsModelConfig{
				Supertonic: sherpa.OfflineTtsSupertonicModelConfig{
					DurationPredictor: filepath.Join(modelRoot, `duration_predictor.int8.onnx`),
					TextEncoder:       filepath.Join(modelRoot, `text_encoder.int8.onnx`),
					VectorEstimator:   filepath.Join(modelRoot, `vector_estimator.int8.onnx`),
					Vocoder:           filepath.Join(modelRoot, `vocoder.int8.onnx`),
					TtsJson:           filepath.Join(modelRoot, `tts.json`),
					UnicodeIndexer:    filepath.Join(modelRoot, `unicode_indexer.bin`),
					VoiceStyle:        filepath.Join(modelRoot, `voice.bin`),
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

	if strings.HasPrefix(model.Name, "matcha") {
		ttsCfg := sherpa.OfflineTtsConfig{
			Model: sherpa.OfflineTtsModelConfig{
				Matcha: sherpa.OfflineTtsMatchaModelConfig{
					AcousticModel: filepath.Join(modelRoot, "model.onnx"), // model-steps-3.onnx
					Vocoder:       filepath.Join(modelRoot, "vocos-22khz-univ.onnx"),
					Tokens:        filepath.Join(modelRoot, "tokens.txt"),
					// Lexicon:       filepath.Join(modelRoot, "lexicon.txt"),
					DataDir:     filepath.Join(modelRoot, "espeak-ng-data"),
					NoiseScale:  0.667,
					LengthScale: 1.0,
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

	ttsCfg := sherpa.OfflineTtsConfig{
		Model: sherpa.OfflineTtsModelConfig{
			Vits: sherpa.OfflineTtsVitsModelConfig{
				Model:   model.ModelFile,
				Tokens:  filepath.Join(modelRoot, "tokens.txt"),
				DataDir: filepath.Join(modelRoot, "espeak-ng-data"),
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
