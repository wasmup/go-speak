package main

import (
	"context"
	"fmt"
	"log"
	"time"
)

type playbackTraceKey struct{}
type playbackTrace struct {
	session         int64
	sentence, total int
}

func withPlaybackTrace(ctx context.Context, session int64, sentence, total int) context.Context {
	return context.WithValue(ctx, playbackTraceKey{}, playbackTrace{session, sentence, total})
}

func traceLog(ctx context.Context, event, format string, args ...any) {
	trace, _ := ctx.Value(playbackTraceKey{}).(playbackTrace)
	log.Printf("playback session=%d sentence=%d/%d event=%s %s", trace.session, trace.sentence, trace.total, event, fmt.Sprintf(format, args...))
}

// Keep a stalled native call or audio wait visible even if it never returns.
func traceStage(ctx context.Context, stage, details string) func(string) {
	start := time.Now()
	traceLog(ctx, stage+"_start", "%s", details)
	done := make(chan struct{})
	go func() {
		ticker := time.NewTicker(5 * time.Second)
		defer ticker.Stop()
		for {
			select {
			case <-done:
				return
			case <-ticker.C:
				traceLog(ctx, stage+"_waiting", "elapsed=%s context_error=%v", time.Since(start).Round(time.Millisecond), ctx.Err())
			}
		}
	}()
	return func(result string) {
		close(done)
		traceLog(ctx, stage+"_end", "elapsed=%s %s", time.Since(start).Round(time.Millisecond), result)
	}
}
