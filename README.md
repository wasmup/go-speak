# Go Speak

An offline text-to-speech player for Linux, with a local web interface, Sherpa-ONNX synthesis, and ALSA playback through `aplay`. Audio plays on the server machine.

Paste text, select a model and speaker, then play, pause, resume, or stop. Progress includes the current sentence, words per minute, pause state, and playback errors. Resuming restarts the interrupted sentence.

## Build and run

Requirements: Linux with glibc, the Go version specified in `go.mod`, a C/C++ compiler, `alsa-utils`, and a compatible TTS model. The Go module supplies native Sherpa-ONNX and ONNX Runtime libraries. Do not use `go mod vendor`: it omits the native libraries needed by these bindings.

```sh
go mod download
go build -trimpath -o build/go-speak .
./build/go-speak -m /opt/go-speak
```

Open <http://127.0.0.1:8080>. Run as your regular desktop user with access to the ALSA device. No root privileges are required.

The application is an unauthenticated, single-user desktop service. It accepts only loopback IP listen addresses and rejects cross-origin browser requests and non-local Host headers. It is not designed for shared hosting or public reverse proxies.

## Models

Download a compatible model from the [Sherpa-ONNX TTS releases](https://github.com/k2-fsa/sherpa-onnx/releases/tag/tts-models), verify its provenance, and extract it under the model root. For a Piper VITS model:

```text
/opt/go-speak/
  vits-piper-en_US-libritts_r-medium/
    en_US-libritts_r-medium.onnx
    tokens.txt
    espeak-ng-data/
```

Each direct child directory is a model. Keep one VITS ONNX graph per directory. Piper VITS is the primary supported layout. Matcha expects an acoustic `model.onnx` or `model-steps-*.onnx` graph, `vocos-22khz-univ.onnx`, `tokens.txt`, and `espeak-ng-data`. Supertonic directories beginning with `sherpa-onnx-supertonic` expect the int8 layout: `duration_predictor.int8.onnx`, `text_encoder.int8.onnx`, `vector_estimator.int8.onnx`, `vocoder.int8.onnx`, `tts.json`, `unicode_indexer.bin`, and `voice.bin`. Kokoro is not supported.

Model loading is lazy, on the first Play request. The active engine is reused and released when switching models or shutting down. A failed load is reported as an HTTP error. Set the speaker ID to a value available in the selected model; unavailable values are rejected before generation.

## Options

```text
-addr       loopback IP and port (default 127.0.0.1:8080)
-m          model root (default /opt/go-speak)
-name       initial model directory name (default first discovered model)
-i          startup UTF-8 text file, at most 256 KiB
-sid        speaker ID (default 0; limited by the selected model)
-speed      speech speed, 0.2–4.0 (default 1.0)
-llm-clean  enable optional local LLM text cleaning (default false)
```

`~` and environment variables are expanded in model and input paths.

LLM cleaning sends candidate sentences to `http://localhost:8011/v1/chat/completions`, requesting model `ornith-1.0-9b`. It has bounded responses, a 20-second timeout, and follows playback cancellation. If cleaning fails, playback uses the original sentence. Default playback makes no LLM requests.

## HTTP API

POST endpoints use `application/x-www-form-urlencoded` bodies. Query parameters do not change settings. All responses disable caching.

| Endpoint | Parameters / response |
| --- | --- |
| `POST /play` | `text`, optional `model_name`, `exclude` (Unicode characters to remove), `split` (sentence delimiter characters); returns `{"text":"original text"}` |
| `POST /pause` | Toggle pause; returns `{"paused":true}` or `false` |
| `POST /stop` | Cancel playback; empty success response |
| `POST /set_sid` | `sid`; empty success response |
| `POST /set_speed` | `speed`; empty success response |
| `GET /progress` | `done`, `total`, `playing`, `paused`, `percent`, `current`, `wpm`, optional `error` |

Text is limited to 256 KiB and the entire form body to 2 MiB. Sentences longer than 1,000 Unicode characters are split into bounded chunks. Invalid input returns 400; oversized text returns 413. Concurrent Play configuration or a Play superseded by Stop returns 409; model initialization failures return 500. Playback failures appear in `/progress` and in the UI.

A new Play cancels the previous session and waits for its worker before reusing or replacing the engine. Native synthesis cannot be interrupted mid-call; Stop cancels audio immediately and discards synthesis results when they return. Long synthesis can therefore delay the next Play. SIGINT and SIGTERM allow up to 15 seconds for shutdown and native cleanup.

## Validation and packaging

```sh
go mod verify
go vet ./...
go test -race ./...
./build-deb.sh
```

The Debian packaging script supports Linux amd64. It writes a `.deb` and SHA-256 checksum under `build/`, bundles the native shared libraries, and installs a wrapper at `/usr/bin/go-speak`. Models are not included. Build on the oldest Debian/Ubuntu release you intend to support and test installation there; native libraries and the compiled executable depend on the target system's glibc and C++ runtime.

CI runs formatting, dependency verification, vet, race tests, package creation, and a packaged executable smoke test. Before releasing, also verify actual synthesis, speaker selection, pause/resume, Stop during generation, model switching, and ALSA playback with your chosen models and audio device. Automated lifecycle tests use a fake speech engine and do not establish model quality or hardware compatibility.

## License

MIT. See [LICENSE](LICENSE). Bundled dependencies retain their own licenses.
