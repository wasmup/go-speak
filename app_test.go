package main

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os/exec"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	sherpa "github.com/k2-fsa/sherpa-onnx-go-linux"
)

type fakeEngine struct {
	generate func()
	closed   atomic.Bool
}

func (e *fakeEngine) Generate(string, int, float32) *sherpa.GeneratedAudio {
	if e.generate != nil {
		e.generate()
	}
	return &sherpa.GeneratedAudio{SampleRate: 16000, Samples: []float32{0, 0.1}}
}
func (e *fakeEngine) NumSpeakers() int { return 2 }
func (e *fakeEngine) Close()           { e.closed.Store(true) }

func testApp(t *testing.T) *App {
	t.Helper()
	a, err := NewApp(&Config{Speed: 1, Models: []Model{{Name: "test"}, {Name: "other"}}})
	if err != nil {
		t.Fatal(err)
	}
	a.newEngine = func(Model) (speechEngine, error) { return &fakeEngine{}, nil }
	a.playAudio = func(ctx context.Context, _ *sherpa.GeneratedAudio) error { return ctx.Err() }
	t.Cleanup(a.Close)
	return a
}
func post(a *App, path string, form url.Values) *httptest.ResponseRecorder {
	r := httptest.NewRequest("POST", "http://127.0.0.1:8080"+path, strings.NewReader(form.Encode()))
	r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	w := httptest.NewRecorder()
	a.Handler().ServeHTTP(w, r)
	return w
}
func waitSignal(t *testing.T, ch <-chan struct{}) {
	t.Helper()
	select {
	case <-ch:
	case <-time.After(3 * time.Second):
		t.Fatal("timed out")
	}
}
func TestInputValidation(t *testing.T) {
	a := testApp(t)
	for _, speed := range []string{"NaN", "+Inf", "-Inf", "0", "4.1", "garbage"} {
		if w := post(a, "/set_speed", url.Values{"speed": {speed}}); w.Code != 400 {
			t.Fatalf("speed %s: %d", speed, w.Code)
		}
	}
	for _, form := range []url.Values{{"text": {" "}}, {"text": {"hello"}, "model_name": {"missing"}}, {"text": {strings.Repeat("x", maxTextBytes+1)}}} {
		if w := post(a, "/play", form); w.Code < 400 {
			t.Fatalf("invalid play accepted: %d", w.Code)
		}
	}
	if got := removeChars("سلام café", "سé"); got != "لام caf" {
		t.Fatalf("Unicode exclusion: %q", got)
	}
}
func TestRequestSecurity(t *testing.T) {
	a := testApp(t)
	for _, tc := range []struct {
		host, origin, site string
		want               int
	}{
		{"evil.example", "", "", 403}, {"127.0.0.1:8080", "http://evil.example", "", 403},
		{"127.0.0.1:8080", "http://127.0.0.1:9090", "", 403}, {"localhost:8080", "", "cross-site", 403},
		{"localhost:8080", "http://localhost:8080", "same-origin", 200},
	} {
		r := httptest.NewRequest("POST", "http://"+tc.host+"/stop", nil)
		r.Header.Set("Origin", tc.origin)
		r.Header.Set("Sec-Fetch-Site", tc.site)
		w := httptest.NewRecorder()
		a.Handler().ServeHTTP(w, r)
		if w.Code != tc.want {
			t.Fatalf("%+v: got %d", tc, w.Code)
		}
	}
	r := httptest.NewRequest("POST", "http://localhost/play", strings.NewReader(strings.Repeat("x", (2<<20)+1)))
	r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	w := httptest.NewRecorder()
	a.Handler().ServeHTTP(w, r)
	if w.Code < 400 {
		t.Fatal("oversized request accepted")
	}
}
func TestModelReuseAndClose(t *testing.T) {
	a := testApp(t)
	var engines []*fakeEngine
	a.newEngine = func(Model) (speechEngine, error) { e := &fakeEngine{}; engines = append(engines, e); return e, nil }
	for _, name := range []string{"test", "test", "other"} {
		if w := post(a, "/play", url.Values{"text": {"hello"}, "model_name": {name}}); w.Code != 200 {
			t.Fatal(w.Body.String())
		}
		a.mu.Lock()
		done := a.done
		a.mu.Unlock()
		waitSignal(t, done)
	}
	if len(engines) != 2 || !engines[0].closed.Load() {
		t.Fatal("model not reused or released")
	}
	a.Close()
	if !engines[1].closed.Load() {
		t.Fatal("model not released at shutdown")
	}
}
func TestStopWhileGeneratingDoesNotStartReplacement(t *testing.T) {
	a := testApp(t)
	entered, release := make(chan struct{}), make(chan struct{})
	var once sync.Once
	a.newEngine = func(Model) (speechEngine, error) {
		return &fakeEngine{generate: func() { once.Do(func() { close(entered); <-release }) }}, nil
	}
	if w := post(a, "/play", url.Values{"text": {"first"}}); w.Code != 200 {
		t.Fatal(w.Body.String())
	}
	waitSignal(t, entered)
	result := make(chan *httptest.ResponseRecorder, 1)
	go func() { result <- post(a, "/play", url.Values{"text": {"second"}}) }()
	// Wait until replacement has canceled the original and is awaiting native generation.
	deadline := time.After(3 * time.Second)
	for {
		a.mu.Lock()
		id := a.sessionID
		a.mu.Unlock()
		if id >= 2 {
			break
		}
		select {
		case <-deadline:
			close(release)
			t.Fatal("replacement did not start")
		default:
			time.Sleep(time.Millisecond)
		}
	}
	post(a, "/stop", nil)
	close(release)
	select {
	case w := <-result:
		if w.Code != 409 {
			t.Fatalf("replacement: %d", w.Code)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("replacement stuck")
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.progress.Playing || a.progress.Done != 0 {
		t.Fatalf("stale progress: %+v", a.progress)
	}
}
func TestPauseStopWakesWorker(t *testing.T) {
	a := testApp(t)
	entered := make(chan struct{})
	a.playAudio = func(ctx context.Context, _ *sherpa.GeneratedAudio) error {
		close(entered)
		<-ctx.Done()
		return ctx.Err()
	}
	post(a, "/play", url.Values{"text": {"hello"}})
	waitSignal(t, entered)
	post(a, "/pause", nil)
	post(a, "/stop", nil)
	a.mu.Lock()
	done := a.done
	a.mu.Unlock()
	waitSignal(t, done)
}
func TestPlaybackFailureIsVisible(t *testing.T) {
	a := testApp(t)
	a.playAudio = func(context.Context, *sherpa.GeneratedAudio) error { return errors.New("device unavailable") }
	post(a, "/play", url.Values{"text": {"hello"}})
	a.mu.Lock()
	done := a.done
	a.mu.Unlock()
	waitSignal(t, done)
	w := httptest.NewRecorder()
	a.handleProgress(w, httptest.NewRequest(http.MethodGet, "/progress", nil))
	var p ProgressResponse
	if err := json.Unmarshal(w.Body.Bytes(), &p); err != nil {
		t.Fatal(err)
	}
	if p.Playing || p.Done != 0 || p.Error == "" {
		t.Fatalf("failure not reported: %+v", p)
	}
	if w := post(a, "/set_sid", url.Values{"sid": {"2"}}); w.Code != 400 {
		t.Fatal("out-of-model SID accepted")
	}
}

func TestConcurrentPlaybackControls(t *testing.T) {
	a := testApp(t)
	var wg sync.WaitGroup
	for i := 0; i < 16; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			post(a, "/play", url.Values{"text": {"First sentence. Second sentence."}})
			post(a, "/pause", nil)
			post(a, "/set_speed", url.Values{"speed": {"1.2"}})
			post(a, "/set_sid", url.Values{"sid": {"1"}})
			w := httptest.NewRecorder()
			a.Handler().ServeHTTP(w, httptest.NewRequest("GET", "http://localhost/progress", nil))
			post(a, "/stop", nil)
		}()
	}
	wg.Wait()
	a.Close()
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.progress.Playing || a.paused {
		t.Fatalf("active state after Close: %+v", a.progress)
	}
}

func TestPlaybackCompletesEverySentence(t *testing.T) {
	a := testApp(t)
	generated, played := 0, 0
	a.newEngine = func(Model) (speechEngine, error) { return &fakeEngine{generate: func() { generated++ }}, nil }
	a.playAudio = func(context.Context, *sherpa.GeneratedAudio) error { played++; return nil }
	if w := post(a, "/play", url.Values{"text": {"First sentence. Second sentence! Third sentence?"}}); w.Code != 200 {
		t.Fatal(w.Body.String())
	}
	a.mu.Lock()
	done := a.done
	a.mu.Unlock()
	waitSignal(t, done)
	a.mu.Lock()
	p := a.progress
	a.mu.Unlock()
	if generated != 3 || played != 3 || p.Done != 3 || p.Total != 3 || p.Playing || p.Error != "" {
		t.Fatalf("generated=%d played=%d progress=%+v", generated, played, p)
	}
}

func TestSaveMP3(t *testing.T) {
	if _, err := exec.LookPath("ffmpeg"); err != nil {
		t.Skip("ffmpeg unavailable")
	}
	a := testApp(t)
	var generated int
	a.newEngine = func(Model) (speechEngine, error) { return &fakeEngine{generate: func() { generated++ }}, nil }
	response := post(a, "/save_mp3", url.Values{"text": {"First sentence. Second sentence."}, "split": {"."}})
	if response.Code != http.StatusOK {
		t.Fatalf("save failed: %d %s", response.Code, response.Body.String())
	}
	if response.Header().Get("Content-Type") != "audio/mpeg" || response.Body.Len() == 0 {
		t.Fatal("missing MP3 response")
	}
	if generated != 2 {
		t.Fatalf("generated %d chunks, want 2", generated)
	}
	cmd := exec.Command("ffmpeg", "-v", "error", "-f", "mp3", "-i", "pipe:0", "-f", "null", "-")
	cmd.Stdin = strings.NewReader(response.Body.String())
	if output, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("invalid MP3: %v: %s", err, output)
	}
}
