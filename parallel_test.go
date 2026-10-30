package main

import (
	"context"
	"testing"
	"time"

	sherpa "github.com/k2-fsa/sherpa-onnx-go-linux"
)

type parallelTestEngine struct {
	started chan string
	release chan struct{}
}

func (e *parallelTestEngine) Generate(text string, _ int, _ float32) *sherpa.GeneratedAudio {
	e.started <- text
	if text == "first" {
		<-e.release
	}
	return &sherpa.GeneratedAudio{SampleRate: 16000, Samples: []float32{0.1}}
}
func (*parallelTestEngine) NumSpeakers() int { return 2 }
func (*parallelTestEngine) Close()           {}

func TestParallelSynthesisConcurrentAndOrdered(t *testing.T) {
	a := testApp(t)
	started := make(chan string, 2)
	release := make(chan struct{})
	factory := func(Model) (speechEngine, error) { return &parallelTestEngine{started, release}, nil }
	a.newEngine = factory
	engine, _ := factory(Model{})
	p := a.startParallelSynthesis(context.Background(), []string{"first", "second"}, Model{}, engine, 2)
	defer p.close()
	for i := 0; i < 2; i++ {
		select {
		case <-started:
		case <-time.After(3 * time.Second):
			close(release)
			t.Fatal("workers did not run concurrently")
		}
	}
	close(release)
	for i, want := range []string{"first", "second"} {
		result, ok := p.get(context.Background(), i)
		if !ok || result.err != nil || result.sentence != want {
			t.Fatalf("result %d: %+v", i, result)
		}
	}
}
