package main

import (
	"encoding/binary"
	"encoding/json"
	"math"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/federico-pepe/ableton-push-hack/core/sse"

	"github.com/federico-pepe/ableton-push-hack/core/alsapcm"
	"monomodule/engine"
	"monomodule/outmix"
	"monomodule/store"
	"monomodule/ui"
)

// newTestApp builds the program around a temp data folder without touching
// hardware.
func newTestApp(t *testing.T) *app {
	t.Helper()
	dir := t.TempDir()
	a := &app{hackDir: dir, dataDir: filepath.Join(dir, "data"), cfg: store.DefaultConfig(), pmURL: "http://127.0.0.1:1"}
	a.eng = engine.New(44100)
	st, err := store.Open(a.dataDir, a.eng)
	if err != nil {
		t.Fatal(err)
	}
	a.st = st
	a.ui = ui.New(a.eng, a)
	a.status.set(false, msgNoCard)
	return a
}

func call(t *testing.T, h http.Handler, method, path, body string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(method, path, strings.NewReader(body))
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)
	return w
}

func TestWebAPI(t *testing.T) {
	a := newTestApp(t)
	ws := &webServer{a: a, broker: sse.NewBroker[[]byte](8, false)}
	h := ws.routes()
	for _, c := range []struct{ m, p, b string }{
		{"POST", "/api/step", `{"track":0,"step":2,"action":"toggle","value":50}`},
		{"POST", "/api/step", `{"track":0,"step":2,"action":"note","value":55}`},
		{"POST", "/api/lock", `{"track":0,"step":2,"param":9,"value":10}`},
		{"POST", "/api/layer", `{"layer":"swing","step":1}`},
		{"POST", "/api/patopt", `{"len":32,"poly":true,"polyTrack":1}`},
		{"POST", "/api/track", `{"track":1,"arp":{"mode":1,"speed":2,"range":2,"len":90,"play":0},"trans":3}`},
		{"POST", "/api/machine", `{"track":2,"machine":8}`},
		{"POST", "/api/param", `{"track":2,"param":3,"value":99}`},
		{"POST", "/api/master", `{"index":0,"value":90}`},
		{"POST", "/api/song", `{"n":0,"rows":[{"Pattern":0,"Repeats":2,"Trans":0}],"loopTo":0}`},
		{"POST", "/api/kit", `{"action":"save","n":3,"name":"Test"}`},
		{"POST", "/api/note", `{"track":2,"note":60,"on":true}`},
		{"POST", "/api/transport", `{"action":"play"}`},
		{"POST", "/api/edit", `{"action":"shift-track","track":0,"value":1}`},
	} {
		if w := call(t, h, c.m, c.p, c.b); w.Code != 200 {
			t.Fatalf("%s %s: %d %s", c.m, c.p, w.Code, w.Body.String())
		}
	}
	for _, c := range []struct{ p, b string }{
		{"/api/step", `{"track":9,"step":0,"action":"toggle"}`},
		{"/api/note", `{"track":0,"note":300,"on":true}`},
		{"/api/pattern", `{"n":500}`},
		{"/api/param", `{"track":0,"param":99}`},
	} {
		if w := call(t, h, "POST", c.p, c.b); w.Code != 400 {
			t.Fatalf("%s should be rejected, got %d", c.p, w.Code)
		}
	}
	w := call(t, h, "GET", "/api/state", "")
	var st map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &st); err != nil {
		t.Fatal(err)
	}
	pat := st["pat"].(map[string]any)
	if pat["len"].(float64) != 32 || pat["poly"] != true {
		t.Fatalf("pattern options: %v", pat)
	}
	grid := st["grid"].([]any)[0].([]any)
	// shifted by one: the trig set on step 2 is now on step 3
	if c := grid[3].(map[string]any); c["on"] != true || c["n"].([]any)[0].(float64) != 55 {
		t.Fatalf("step: %v", c)
	}
	if w := call(t, h, "GET", "/api/machines", ""); w.Code != 200 || !strings.Contains(w.Body.String(), "SWAVE-SAW") {
		t.Fatal("machines")
	}
	if w := call(t, h, "GET", "/", ""); w.Code != 200 || !strings.Contains(w.Body.String(), "MonoModule") {
		t.Fatal("index page")
	}
	proj := call(t, h, "GET", "/api/project", "")
	if proj.Code != 200 || !strings.Contains(proj.Body.String(), "monomodule-1") {
		t.Fatal("project download")
	}
	if w := call(t, h, "POST", "/api/project", proj.Body.String()); w.Code != 200 {
		t.Fatalf("project restore: %d %s", w.Code, w.Body.String())
	}
	if w := call(t, h, "POST", "/api/project", `{"format":"nope"}`); w.Code != 422 {
		t.Fatal("bad project accepted")
	}
}

// TestAudioPath runs the real session code (cgo bridge and render loop)
// against a stand-in for libasound that writes the PCM to $FAKE_PCM_OUT.
// It only runs when that variable is set; see scripts/smoke.sh.
func TestAudioPath(t *testing.T) {
	out := os.Getenv("FAKE_PCM_OUT")
	if out == "" {
		t.Skip("FAKE_PCM_OUT not set (needs the libasound stand-in)")
	}
	a := newTestApp(t)
	a.eng.Play()
	hp := alsapcm.HWParams{Channels: 8, Rate: 48000, Period: 512, Buffer: 2048}
	a.cfg.ChannelOffset = 2
	a.cfg.OutputMode = outmix.Stereo
	a.storeMirror(a.cfg)
	s, err := a.startSession(hp, "hw:fake")
	if err != nil {
		t.Fatal(err)
	}
	time.Sleep(1500 * time.Millisecond)
	s.close()
	b, err := os.ReadFile(out)
	if err != nil || len(b) < 48000*8*2/2 {
		t.Fatalf("pcm output: %v, %d bytes", err, len(b))
	}
	var peak [8]float64
	for i := 0; i+1 < len(b); i += 2 {
		v := math.Abs(float64(int16(binary.LittleEndian.Uint16(b[i:]))))
		if ch := (i / 2) % 8; v > peak[ch] {
			peak[ch] = v
		}
	}
	for ch := range peak {
		if (ch == 2 || ch == 3) != (peak[ch] > 500) {
			t.Fatalf("channel %d peak %v: sound must be on the offset pair 3/4 only", ch, peak)
		}
	}
}

// TestMonoOutputSwitchesLive changes mode and channel while the session runs.
func TestMonoOutputSwitchesLive(t *testing.T) {
	out := os.Getenv("FAKE_PCM_OUT")
	if out == "" {
		t.Skip("FAKE_PCM_OUT not set (needs the libasound stand-in)")
	}
	a := newTestApp(t)
	a.eng.Play()
	s, err := a.startSession(alsapcm.HWParams{Channels: 8, Rate: 48000, Period: 512, Buffer: 2048}, "hw:fake")
	if err != nil {
		t.Fatal(err)
	}
	time.Sleep(600 * time.Millisecond)
	_ = a.setConfig(func(c *store.Config) { c.OutputMode, c.ChannelOffset = outmix.Mono, 2 })
	time.Sleep(600 * time.Millisecond)
	s.close()
	b, _ := os.ReadFile(out)
	frames := len(b) / 16
	if frames < 48000/2 {
		t.Fatalf("only %d frames", frames)
	}
	// the last quarter of the stream is mono on channel 3 (index 2)
	var peak [8]float64
	for f := frames * 3 / 4; f < frames; f++ {
		for ch := 0; ch < 8; ch++ {
			v := math.Abs(float64(int16(binary.LittleEndian.Uint16(b[(f*8+ch)*2:]))))
			if v > peak[ch] {
				peak[ch] = v
			}
		}
	}
	for ch := range peak {
		if (ch == 2) != (peak[ch] > 500) {
			t.Fatalf("channel %d peak %v", ch, peak)
		}
	}
}
