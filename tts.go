package main

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	sherpa "github.com/k2-fsa/sherpa-onnx-go-linux"
)

type nativeEngine struct{ *sherpa.OfflineTts }

func (e *nativeEngine) Close() { sherpa.DeleteOfflineTts(e.OfflineTts) }

func NewTTS(model Model) (*sherpa.OfflineTts, error) {
	modelRoot := filepath.Dir(model.ModelFile)
	// Check required assets before entering native code, which cannot report Go
	// errors for malformed model layouts.
	required := []string{model.ModelFile, filepath.Join(modelRoot, "tokens.txt"), filepath.Join(modelRoot, "espeak-ng-data")}
	if strings.HasPrefix(model.Name, "kokoro") {
		required = append(required, filepath.Join(modelRoot, "voices.bin"))
	}
	if strings.HasPrefix(model.Name, "sherpa-onnx-supertonic") {
		required = nil
		for _, name := range []string{"duration_predictor.int8.onnx", "text_encoder.int8.onnx", "vector_estimator.int8.onnx", "vocoder.int8.onnx", "tts.json", "unicode_indexer.bin", "voice.bin"} {
			required = append(required, filepath.Join(modelRoot, name))
		}
	} else if strings.HasPrefix(model.Name, "matcha") {
		required = append(required, filepath.Join(modelRoot, "vocos-22khz-univ.onnx"))
	}
	for _, path := range required {
		info, err := os.Stat(path)
		if err != nil {
			return nil, fmt.Errorf("model asset %s: %w", path, err)
		}
		if filepath.Base(path) == "espeak-ng-data" {
			if !info.IsDir() {
				return nil, fmt.Errorf("model asset %s must be a directory", path)
			}
		} else if !info.Mode().IsRegular() {
			return nil, fmt.Errorf("model asset %s must be a regular file", path)
		}
		if !info.IsDir() && info.Size() == 0 {
			return nil, fmt.Errorf("empty model asset %s", path)
		}
	}

	if strings.HasPrefix(model.Name, "kokoro") {
		kokoro := sherpa.OfflineTtsKokoroModelConfig{
			Model: model.ModelFile, Voices: filepath.Join(modelRoot, "voices.bin"),
			Tokens: filepath.Join(modelRoot, "tokens.txt"), DataDir: filepath.Join(modelRoot, "espeak-ng-data"), LengthScale: 1,
		}
		// Multilingual distributions include optional dictionaries and lexicons.
		if info, err := os.Stat(filepath.Join(modelRoot, "dict")); err == nil && info.IsDir() {
			kokoro.DictDir = filepath.Join(modelRoot, "dict")
		}
		var lexicons []string
		for _, name := range []string{"lexicon-us-en.txt", "lexicon-gb-en.txt", "lexicon-zh.txt"} {
			path := filepath.Join(modelRoot, name)
			if info, err := os.Stat(path); err == nil && info.Mode().IsRegular() {
				lexicons = append(lexicons, path)
			}
		}
		kokoro.Lexicon = strings.Join(lexicons, ",")
		cfg := sherpa.OfflineTtsConfig{Model: sherpa.OfflineTtsModelConfig{Kokoro: kokoro, NumThreads: 4}}
		tts := sherpa.NewOfflineTts(&cfg)
		if tts == nil {
			return nil, errors.New("failed to initialize Kokoro TTS")
		}
		return tts, nil
	}

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
					AcousticModel: model.ModelFile,
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
// It returns one unambiguous model graph per child directory, excluding
// auxiliary graphs for supported multi-file layouts.
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

		var candidates []string
		for _, child := range childEntries {
			if child.IsDir() {
				continue
			}

			if strings.EqualFold(filepath.Ext(child.Name()), ".onnx") {
				// Multi-file models have one discovery entry, not one per graph.
				if strings.HasPrefix(entry.Name(), "kokoro") && child.Name() != "model.onnx" {
					continue
				}
				if strings.HasPrefix(entry.Name(), "sherpa-onnx-supertonic") && child.Name() != "duration_predictor.int8.onnx" {
					continue
				}
				if strings.HasPrefix(entry.Name(), "matcha") && child.Name() != "model.onnx" && !strings.HasPrefix(child.Name(), "model-steps-") {
					continue
				}
				fullPath := filepath.Join(dirPath, child.Name())

				// normalization to absolute path
				absPath, err := filepath.Abs(fullPath)
				if err != nil {
					return nil, err
				}

				candidates = append(candidates, absPath)
			}
		}
		if len(candidates) > 1 && strings.HasPrefix(entry.Name(), "matcha") {
			for _, path := range candidates {
				if filepath.Base(path) == "model.onnx" {
					candidates = []string{path}
					break
				}
			}
		}
		if len(candidates) > 1 {
			return nil, fmt.Errorf("model directory %s contains multiple candidate ONNX graphs; keep one model per directory", dirPath)
		}
		out = append(out, candidates...)
	}

	return out, nil
}
