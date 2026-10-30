package main

import (
	"bytes"
	"context"
	"encoding/binary"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	sherpa "github.com/k2-fsa/sherpa-onnx-go-linux"
)

// Prefer the native PipeWire client when a session socket exists. Routing
// through PipeWire's ALSA plugin can hang in aplay's drain after stdin EOF.
func resolvePlayer(requested string) (string, error) {
	if requested == "" || requested == "auto" {
		requested = "aplay"
		runtimeDir := os.Getenv("PIPEWIRE_RUNTIME_DIR")
		if runtimeDir == "" {
			runtimeDir = os.Getenv("XDG_RUNTIME_DIR")
		}
		remote := os.Getenv("PIPEWIRE_REMOTE")
		if remote == "" {
			remote = "pipewire-0"
		}
		socket := remote
		if !filepath.IsAbs(socket) {
			if runtimeDir == "" {
				socket = ""
			} else {
				socket = filepath.Join(runtimeDir, remote)
			}
		}
		if socket != "" {
			if info, err := os.Stat(socket); err == nil && info.Mode()&os.ModeSocket != 0 {
				if _, err := exec.LookPath("pw-play"); err == nil {
					requested = "pw-play"
				}
			}
		}
	}
	if requested != "aplay" && requested != "pw-play" {
		return "", fmt.Errorf("invalid audio player %q; use auto, aplay, or pw-play", requested)
	}
	if _, err := exec.LookPath(requested); err != nil {
		return "", fmt.Errorf("audio player %s: %w", requested, err)
	}
	return requested, nil
}

func play(ctx context.Context, audio *sherpa.GeneratedAudio) error {
	return playWithPlayer(ctx, audio, "auto")
}

func playWithPlayer(ctx context.Context, audio *sherpa.GeneratedAudio, requested string) error {
	if audio == nil || audio.SampleRate <= 0 || len(audio.Samples) == 0 {
		return fmt.Errorf("invalid audio")
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	player, err := resolvePlayer(requested)
	if err != nil {
		return err
	}
	args := []string{"-q", "-t", "raw", "-f", "S16_LE", "-r", fmt.Sprint(audio.SampleRate), "-c", "1"}
	if player == "pw-play" {
		args = []string{"--raw", "--format", "s16", "--rate", fmt.Sprint(audio.SampleRate), "--channels", "1", "-"}
	}
	return runPlayer(ctx, player, args, audio, 10*time.Second)
}

func runPlayer(ctx context.Context, player string, args []string, audio *sherpa.GeneratedAudio, grace time.Duration) (result error) {
	// The expected audio duration plus a grace period bounds both pipe writes
	// and device draining. Parent cancellation still stops pause/stop immediately.
	duration := time.Duration(float64(len(audio.Samples)) / float64(audio.SampleRate) * float64(time.Second))
	playbackCtx, cancel := context.WithTimeout(ctx, duration+grace)
	defer cancel()
	cmd := exec.CommandContext(playbackCtx, player, args...)
	cmd.WaitDelay = time.Second
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	pcm := make([]byte, 2*len(audio.Samples))
	for i, sample := range audio.Samples {
		if sample > 1 {
			sample = 1
		} else if sample < -1 {
			sample = -1
		}
		binary.LittleEndian.PutUint16(pcm[2*i:], uint16(int16(sample*32767)))
	}
	cmd.Stdin = &tracedPCMReader{ctx: ctx, reader: bytes.NewReader(pcm), total: len(pcm)}
	finishProcess := traceStage(ctx, "audio_process", fmt.Sprintf("player=%q executable=%q args=%q samples=%d sample_rate=%d expected_duration=%s timeout=%s pcm_bytes=%d", player, cmd.Path, args, len(audio.Samples), audio.SampleRate, duration.Round(time.Millisecond), (duration+grace).Round(time.Millisecond), len(pcm)))
	defer func() { finishProcess(fmt.Sprintf("error=%v stderr=%q", result, stderr.String())) }()
	if err := cmd.Start(); err != nil {
		return fmt.Errorf("start %s: %w", player, err)
	}
	traceLog(ctx, "audio_process_started", "pid=%d player=%q", cmd.Process.Pid, player)
	err := cmd.Wait()
	traceLog(ctx, "audio_process_exited", "pid=%d state=%q error=%v", cmd.Process.Pid, cmd.ProcessState.String(), err)
	if ctx.Err() != nil {
		return ctx.Err()
	}
	if playbackCtx.Err() != nil {
		return fmt.Errorf("%s playback timed out after %s: %w", player, duration+grace, playbackCtx.Err())
	}
	if err != nil {
		if message := strings.TrimSpace(stderr.String()); message != "" {
			return fmt.Errorf("%s: %w: %s", player, err, message)
		}
		return fmt.Errorf("%s: %w", player, err)
	}
	return nil
}

// io.Copy asks for EOF only after successfully writing the preceding PCM block.
// Seeing this event followed by audio_process_waiting isolates a drain/exit hang.
type tracedPCMReader struct {
	ctx    context.Context
	reader *bytes.Reader
	total  int
	logged bool
}

func (r *tracedPCMReader) Read(p []byte) (int, error) {
	n, err := r.reader.Read(p)
	if err == io.EOF && !r.logged {
		r.logged = true
		traceLog(r.ctx, "audio_input_written", "pcm_bytes=%d stdin_eof=true", r.total)
	}
	return n, err
}
