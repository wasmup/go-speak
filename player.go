package main

import (
	"context"
	"encoding/binary"
	"fmt"
	"os/exec"

	sherpa "github.com/k2-fsa/sherpa-onnx-go-linux"
)

func play(ctx context.Context, audio *sherpa.GeneratedAudio) error {
	cmd := exec.CommandContext(ctx,
		"aplay",
		"-f", "S16_LE",
		"-r", fmt.Sprint(audio.SampleRate),
		"-c", "1",
	)

	stdin, err := cmd.StdinPipe()
	if err != nil {
		return err
	}

	if err := cmd.Start(); err != nil {
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
		return nil
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
		return nil
	}

	return err
}
