package main

import (
	"context"
	"fmt"
	"log"
	"sync"
	"time"

	sherpa "github.com/k2-fsa/sherpa-onnx-go-linux"
)

type synthesisResult struct {
	sentence string
	audio    *sherpa.GeneratedAudio
	err      error
}

type parallelSynthesis struct {
	cancel  context.CancelFunc
	jobs    chan int
	results []chan synthesisResult
	wg      sync.WaitGroup
	next    int
}

// A bounded window limits buffered audio, even if a later sentence finishes first.
func (a *App) startParallelSynthesis(ctx context.Context, sentences []string, model Model, engine speechEngine, workers int) *parallelSynthesis {
	ctx, cancel := context.WithCancel(ctx)
	p := &parallelSynthesis{cancel: cancel, jobs: make(chan int, workers), results: make([]chan synthesisResult, len(sentences))}
	for i := range p.results {
		p.results[i] = make(chan synthesisResult, 1)
	}
	sid, speed := int(a.sid.Load()), float32(a.speedX100.Load())/100
	for worker := 0; worker < workers; worker++ {
		p.wg.Add(1)
		go func(worker int) {
			defer p.wg.Done()
			local := engine
			var initErr error
			if worker != 0 {
				local, initErr = a.newEngine(model)
				if initErr == nil {
					defer local.Close()
				}
			}
			for i := range p.jobs {
				if ctx.Err() != nil {
					return
				}
				sentence := sentences[i]
				if a.cfg.LLMClean && needsLLMClean(sentence) {
					cleaned, err := Clean(ctx, sentence)
					if err == nil {
						sentence = cleaned
					} else if ctx.Err() == nil {
						log.Printf("text cleaning failed: %v", err)
					}
				}
				started := time.Now()
				result := synthesisResult{sentence: sentence, err: initErr}
				if result.err == nil {
					if contextual, ok := local.(interface {
						GenerateContext(context.Context, string, int, float32) (*sherpa.GeneratedAudio, error)
					}); ok {
						result.audio, result.err = contextual.GenerateContext(ctx, sentence, sid, speed)
					} else {
						result.audio = local.Generate(sentence, sid, speed)
					}
				} else {
					result.err = fmt.Errorf("initialize export worker: %w", result.err)
				}
				log.Printf("export event=parallel_generate_end sentence=%d elapsed=%s error=%v", i+1, time.Since(started), result.err)
				select {
				case p.results[i] <- result:
				case <-ctx.Done():
					return
				}
			}
		}(worker)
	}
	for p.next < workers && p.next < len(sentences) {
		p.jobs <- p.next
		p.next++
	}
	return p
}

func (p *parallelSynthesis) get(ctx context.Context, i int) (synthesisResult, bool) {
	select {
	case result := <-p.results[i]:
		if p.next < len(p.results) {
			p.jobs <- p.next
			p.next++
		}
		return result, true
	case <-ctx.Done():
		return synthesisResult{}, false
	}
}
func (p *parallelSynthesis) close() { p.cancel(); close(p.jobs); p.wg.Wait() }
