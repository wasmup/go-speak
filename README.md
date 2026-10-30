# Go Speak

An offline text-to-speech player for Linux, with a local web interface, Qwen3-TTS or Sherpa-ONNX synthesis, and audio playback through native PipeWire (`pw-play`) or ALSA (`aplay`). Audio plays on the server machine.

Paste text, select a model and speaker, then play, pause, resume, or stop. Progress includes the current sentence, words per minute, pause state, and playback errors. Resuming restarts the interrupted sentence.

## Build and run

Requirements: Linux with glibc, the Go version specified in `go.mod`, a C/C++ compiler, an audio player (`pw-play` for PipeWire desktops or `aplay` from `alsa-utils` for ALSA systems), and a compatible TTS model. MP3 export also requires `ffmpeg` on PATH. Use **Save MP3** to download the generated speech using the selected model and settings. The suggested filename uses the audio duration. The Go module supplies native Sherpa-ONNX and ONNX Runtime libraries. Do not use `go mod vendor`: it omits the native libraries needed by these bindings.

Download the default Supertonic model into a directory owned by your user. This URL is the archive used in the [Sherpa-ONNX Supertonic instructions](https://github.com/k2-fsa/sherpa/blob/master/docs/source/onnx/tts/supertonic.rst). Check the model's license and provenance before use.

```sh
mkdir -p "$HOME/tts"
curl -fL -o /tmp/supertonic-3.tar.bz2 https://github.com/k2-fsa/sherpa-onnx/releases/download/tts-models/sherpa-onnx-supertonic-3-tts-int8-2026-05-11.tar.bz2
tar -xjf /tmp/supertonic-3.tar.bz2 -C "$HOME/tts"
go mod download
go build -trimpath -o build/go-speak .
./build/go-speak -m "$HOME/tts"
```

This requires `curl`, `tar`, and bzip2 support. The extracted directory must be `$HOME/tts/sherpa-onnx-supertonic-3-tts-int8-2026-05-11`. Without `-m`, the model root is `/opt/go-speak`. If the default model is absent, startup fails: install it or specify another installed directory name with `-name`.

Open <http://127.0.0.1:8080>. Run as your regular desktop user with access to your desktop audio session or ALSA device. No root privileges are required.

The application is an unauthenticated, single-user desktop service. It accepts only loopback IP listen addresses and rejects cross-origin browser requests and non-local Host headers. It is not designed for shared hosting or public reverse proxies.

## Models

The default is **sherpa-onnx-supertonic-3-tts-int8-2026-05-11**. Install that model under the model root (default `/opt/go-speak`), or select another installed model with `-name`.

Qwen3-TTS-12Hz-1.7B-CustomVoice is also available, using English and speaker ID 4 (Aiden). It runs through a persistent Python worker, loaded on the first Play request. The model stays loaded between sentences. Stop cancels generation by terminating the worker; the next Play reloads it.

Install [uv](https://docs.astral.sh/uv/) and run the CPU setup once:

```sh
./setup-qwen.sh
go build -trimpath -o build/go-speak .
./build/go-speak -name Qwen3-TTS-12Hz-1.7B-CustomVoice
```

Setup downloads Python 3.12, CPU PyTorch, `qwen-tts`, and the model weights into `build/`. Downloads require internet access; generation uses local weights afterward. The app automatically finds `build/qwen-venv` and `build/qwen-model` from the project directory or beside the executable. No ONNX model directory is needed for Qwen. CPU synthesis can be slow; each sentence has a 20-minute deadline including first-load time.

For another installation, use `-qwen-python /path/to/venv/bin/python -qwen-model /path/to/model`. `-qwen-device auto` selects CUDA when available, otherwise CPU. The setup script installs CPU wheels; GPU use requires an appropriate CUDA PyTorch installation in that environment. See the [official Qwen setup](https://github.com/QwenLM/Qwen3-TTS).

Qwen speaker IDs: 0 Ryan, 1 Vivian, 2 Serena, 3 Uncle_Fu, 4 Aiden, 5 Dylan, 6 Eric, 7 Ono_Anna, 8 Sohee. Ryan and Aiden are native English voices. Speed is requested through a natural-language instruction and is approximate, rather than an exact audio time stretch.

Select a Sherpa model with `-name MODEL_DIRECTORY_NAME` or through the model selector. For Sherpa models:

Download a compatible model from the [Sherpa-ONNX TTS releases](https://github.com/k2-fsa/sherpa-onnx/releases/tag/tts-models), verify its provenance, and extract it under the model root. For a Piper VITS model:

```text
/opt/go-speak/
  vits-piper-en_US-libritts_r-medium/
    en_US-libritts_r-medium.onnx
    tokens.txt
    espeak-ng-data/
```

Each direct child directory is a model. Keep one VITS ONNX graph per directory. Piper VITS is the primary supported layout. Matcha expects an acoustic `model.onnx` or `model-steps-*.onnx` graph, `vocos-22khz-univ.onnx`, `tokens.txt`, and `espeak-ng-data`. Supertonic directories beginning with `sherpa-onnx-supertonic` expect the int8 layout: `duration_predictor.int8.onnx`, `text_encoder.int8.onnx`, `vector_estimator.int8.onnx`, `vocoder.int8.onnx`, `tts.json`, `unicode_indexer.bin`, and `voice.bin`. Kokoro directories beginning with `kokoro` use `model.onnx`, `voices.bin`, `tokens.txt`, and `espeak-ng-data`; optional multilingual dictionaries and lexicons are loaded when present. Extra ONNX files in Kokoro directories are ignored.

For the Piper layout above, start with `./build/go-speak -m /opt/go-speak -name vits-piper-en_US-libritts_r-medium`. The model selector can switch models after the server starts.

Model loading is lazy, on the first Play request. The active engine is reused and released when switching models or shutting down. A failed load is reported as an HTTP error. Set the speaker ID to a value available in the selected model; unavailable values are rejected before generation.

## Options

```text
-addr       loopback IP and port (default 127.0.0.1:8080)
-m          model root (default /opt/go-speak)
-name       initial model name (default sherpa-onnx-supertonic-3-tts-int8-2026-05-11)
-qwen-python Python executable with qwen-tts (auto-detected local venv, otherwise python3)
-qwen-model local Qwen CustomVoice directory or Hugging Face ID
-qwen-device torch device (default auto; cpu or cuda:0 supported)
-i          startup UTF-8 text file, at most 256 KiB
-sid        speaker ID (default 4; limited by the selected model)
-speed      speech speed, 0.2–4.0 (default 1.1)
-llm-clean  enable optional local LLM text cleaning (default false)
-player     auto, pw-play, or aplay (default auto)
```

`~` and environment variables are expanded in model and input paths.

Automatic audio selection uses `pw-play` when its executable and a PipeWire session socket are available, otherwise `aplay`. Native PipeWire playback avoids an ALSA-plugin drain hang that can leave playback stuck after the first sentence. Override selection with `-player pw-play` or `-player aplay`. Audio processes have a deadline equal to clip duration plus 10 seconds; device errors and timeouts appear in progress and the UI.

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

## Playback diagnostics

Playback logs are written to stderr with session and sentence numbers. Capture a run with:

```sh
go-speak -m ~/tts 2>&1 | tee /tmp/go-speak.log
```

Startup includes `diagnostics=playback-trace-v1`, the executable path, and selected player. Each sentence reports `generate_start/end`, `audio_process_started`, `audio_input_written`, `audio_process_exited`, and `sentence_complete`. Operations still running after five seconds emit `_waiting` events. Model loading, previous-worker waits, pause/resume, cancellation, and session completion are also logged. Sentence text is not included.

If playback stalls, the last stage and its `_waiting` messages identify whether generation, audio delivery, or process exit is blocked. After `audio_input_written`, an `audio_process_waiting` event means all PCM was sent but the player has not exited.

## Profiling MP3 export

Profiling is controlled by the `RunProfiling` constant in `profiling.go` (currently `true`). Set it to `false` and rebuild to disable profiling. Each MP3 export starts a CPU profile and saves it along with a heap profile when conversion finishes, before the download begins. Files are overwritten by the next export and are written beside the executable as `go-speak-cpu.pprof`, `go-speak-heap.pprof`, `go-speak-goroutines-before.pprof`, and `go-speak-goroutine.pprof`; that directory must be writable. Failures are logged and do not prevent the export.

For a representative measurement, build into a writable directory and run:

```sh
go build -o /tmp/go-speak-profile .
/tmp/go-speak-profile -m ~/tts 2>&1 | tee /tmp/go-speak-profile.log
```

Uncheck **Play sound while saving**, export representative text, and wait for the download. Inspect the resulting profiles with:

```sh
go tool pprof -http=':8787' build/go-speak-cpu.pprof
go tool pprof -http=':8788' build/go-speak-heap.pprof
go tool pprof -http=':8789' build/go-speak-goroutines-before.pprof
go tool pprof -http=':8790' build/go-speak-goroutine.pprof
go tool pprof -http=:8791 -base build/go-speak-goroutines-before.pprof build/go-speak-goroutine.pprof

```

For exports started with sound off, **Workers** sets the maximum number of independent TTS engines, from 1 to the detected CPU count. Results are assembled in sentence order. The setting is fixed when an export starts, including if sound is toggled later. Each engine uses additional memory and its own inference threads, so a high worker count can be slower or exhaust memory; benchmark and reduce it if needed. Exports started with sound on use one engine.

The goroutine profiles are snapshots at export start and finish. Compare repeated completed exports to investigate stacks that accumulate; a single difference can also reflect normal HTTP and background activity, and canceled exports can still have native synthesis finishing in the background. Save copies between runs because each export overwrites the profiles.

The log's `generate_end` (or `parallel_generate_end` for parallel exports) elapsed time covers synthesis per sentence, `audio_end` covers handling each generated chunk (including speaker playback when enabled), and `encode_end` reports the complete ffmpeg encoding time. Model loading is logged separately; compare a first export and a subsequent export to distinguish startup from steady-state costs. Silent export is best for measuring conversion throughput.

Go CPU profiles cover the Go process, but may not resolve work inside native Sherpa/ONNX libraries; they do not profile the separate Qwen Python worker or ffmpeg process. The heap profile covers live Go-managed allocations at the end of export, not native, Python, or ffmpeg memory. Use the stage timings to identify expensive external or native work, then use platform-specific profilers for those processes if needed. Profile files and logs should be kept locally because they can reveal application details.

---

The profiles point to **native TTS inference as the main CPU cost**. The capture lasted 35.1 seconds and recorded 169.9 CPU-seconds across threads—about 4.8 CPU cores on average.

- **80% of sampled CPU time** appeared in `libonnxruntime.so`.
- Another **19% appeared in `runtime.cgocall`**, mostly on the Sherpa TTS generation call path. The profile cannot resolve exactly how that time is spent inside native code.
- The Go PCM conversion and MP3 handling did not appear among the significant CPU samples. The separate ffmpeg process is not included in this profile, so its encoding time needs to be checked in the `encode_end` log entry.

The heap profile shows about **75.6 MB of sampled cumulative Go allocations**, including approximately 43.1 MB attributed to writing PCM samples and 21.1 MB to returned Sherpa audio. At the final heap snapshot, about **3 MB of sampled Go memory** remained live. These figures exclude native ONNX memory and the ffmpeg process, and cumulative allocations cover the process lifetime rather than just this export.

The best next experiment is to benchmark inference settings: compare the current four ONNX threads per model with other thread counts, then compare a small pool of separate engines for silent export. More concurrency may help, but the current run already uses several cores, so it could also cause contention. Measure elapsed export time and memory for the same text and model; the profiles alone can’t establish which configuration will be faster.

As a secondary improvement, the PCM loop in [web.go](web.go) writes one sample at a time. Writing blocks would reduce allocations, though this profile suggests it is unlikely to be the main speedup.

---

## Validation and packaging

```sh
go mod verify
go vet ./...
go test -race ./...
./build-deb.sh
```

The Debian packaging script supports Linux amd64. It writes a `.deb` and SHA-256 checksum under `build/`, bundles the native shared libraries, and installs a wrapper at `/usr/bin/go-speak`. Models are not included. Build on the oldest Debian/Ubuntu release you intend to support and test installation there; native libraries and the compiled executable depend on the target system's glibc and C++ runtime.

On a compatible Debian or Ubuntu machine, install a built package and use the model downloaded above:

```sh
sudo apt install ./build/go-speak-1.0.5-linux-amd64.deb
go-speak -m "$HOME/tts"
```

Use the actual package filename if you built with a different `VERSION`. To validate a release, install the package on a clean target system, start it with the default Supertonic model, open the web interface, and play a short sentence. Confirm that audio is audible and `/progress` reports completion without an error; also check pause and stop. The CI package smoke test only runs `-h` and does not perform synthesis.

CI runs formatting, dependency verification, vet, race tests, package creation, and a packaged executable smoke test. Before releasing, also verify actual synthesis, speaker selection, pause/resume, Stop during generation, model switching, and ALSA playback with your chosen models and audio device. Automated lifecycle tests use a fake speech engine. To test a real Kokoro model, including the four-sentence regression text, run:

```sh
GO_SPEAK_KOKORO_MODEL="$HOME/tts/kokoro-en-v0_19" go test -run TestKokoroMultipleSentencePlayback -v
```

That test sends audio to ALSA's null device. Add `GO_SPEAK_TEST_AUDIO_DEVICE=1` to exercise native PipeWire playback instead, with output volume set to zero. These checks do not establish audible quality or compatibility with every audio device.

## License

MIT. See [LICENSE](LICENSE). Bundled dependencies retain their own licenses.

The embedded `dict/words_alpha.txt` comes from [dwyl/english-words](https://github.com/dwyl/english-words). That repository displays an Unlicense while its README also attributes copyright in the source word list to another party. Its redistribution terms require clarification before distributing this project's source or binaries; do not assume the project's MIT license covers that data. Model weights are downloaded separately and have their own terms.
