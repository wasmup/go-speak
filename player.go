package main

import (
	"context"
	"encoding/binary"
	"fmt"
	"os/exec"

	sherpa "github.com/k2-fsa/sherpa-onnx-go-linux"
)

func play(ctx context.Context, audio *sherpa.GeneratedAudio) error {
	if audio == nil || audio.SampleRate <= 0 || len(audio.Samples) == 0 {
		return fmt.Errorf("invalid audio")
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	cmd := exec.CommandContext(ctx,
		"aplay",
		"-q", "-t", "raw",
		"-f", "S16_LE",
		"-r", fmt.Sprint(audio.SampleRate),
		"-c", "1",
	)

	stdin, err := cmd.StdinPipe()
	if err != nil {
		return err
	}

	defer stdin.Close()

	if err := cmd.Start(); err != nil {
		stdin.Close()
		return err
	}

	buf := make([]int16, len(audio.Samples))
	for i, f32 := range audio.Samples {
		if f32 > 1 {
			f32 = 1
		} else if f32 < -1 {
			f32 = -1
		}
		buf[i] = int16(f32 * 32767)
	}

	if ctx.Err() != nil {
		cmd.Process.Kill()
		cmd.Wait()
		return ctx.Err()
	}

	if err := binary.Write(stdin, binary.LittleEndian, buf); err != nil {
		stdin.Close()
		cmd.Process.Kill()
		cmd.Wait()
		return err
	}

	if err := stdin.Close(); err != nil {
		cmd.Process.Kill()
		cmd.Wait()
		return err
	}

	err = cmd.Wait()
	if ctx.Err() != nil {
		return ctx.Err()
	}

	return err
}
