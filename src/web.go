package main

import (
	"context"
	"embed"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"strconv"
	"sync/atomic"
	"time"

	"github.com/federico-pepe/ableton-push-hack/core/alsapcm"
	"github.com/federico-pepe/ableton-push-hack/core/httpx"
	"github.com/federico-pepe/ableton-push-hack/core/sse"

	"monomodule/audioout"
	"monomodule/engine"
	"monomodule/livelink"
	"monomodule/outmix"
	"monomodule/store"
)

//go:embed web/index.html
var webFS embed.FS

type webServer struct {
	a       *app
	broker  *sse.Broker[[]byte]
	clients atomic.Int32 // open /sse/state streams
}

type waveInfo struct {
	Slot int    `json:"slot"`
	Name string `json:"name"`
	Rate int    `json:"rate"`
	Len  int    `json:"len"`
}

func (w *webServer) state() map[string]any {
	a := w.a
	e := a.eng
	pos := e.Position()
	var pat engine.Pattern
	e.ReadCurrent(func(p *engine.Pattern) { pat = *p })
	ok, msg := a.status.get()
	tracks := make([]map[string]any, engine.NumTracks)
	for t := range tracks {
		var p [engine.NParams]int
		for i := range p {
			p[i] = e.Param(t, i)
		}
		m := e.Machine(t)
		ts := e.TrackSetup(t)
		tracks[t] = map[string]any{"machine": m, "name": engine.Machines[m].Name,
			"mute": e.Muted(t), "peak": e.Peak(t), "params": p,
			"keyLP": ts.KeyLP, "keyHP": ts.KeyHP, "src": ts.Src, "trans": ts.Trans, "transFix": ts.TransFix,
			"arp": map[string]int{"mode": ts.Arp.Mode, "speed": ts.Arp.Speed, "range": ts.Arp.Range,
				"len": ts.Arp.Len, "play": ts.Arp.Play}}
	}
	type stepJSON struct {
		On    bool  `json:"on"`
		Off   bool  `json:"off,omitempty"`
		Notes []int `json:"n,omitempty"`
		Vel   uint8 `json:"v,omitempty"`
		Len   uint8 `json:"l,omitempty"`
		Trig  uint8 `json:"x,omitempty"`
		Lock  bool  `json:"k,omitempty"`
	}
	grid := make([][]stepJSON, engine.NumTracks)
	for t := range grid {
		grid[t] = make([]stepJSON, pat.Len)
		for s := range grid[t] {
			st := pat.Tracks[t].Step[s]
			if !st.On {
				continue
			}
			var b [engine.MaxChord]uint8
			n := st.Notes(&b)
			grid[t][s] = stepJSON{On: true, Off: st.Off, Notes: ints(b[:n]), Vel: st.Vel, Len: st.Len,
				Trig: st.Trig, Lock: pat.Tracks[t].Lock[s] != nil}
		}
	}
	var master [engine.NMaster]int
	for i := range master {
		master[i] = e.Master(i)
	}
	swing, slide := []int{}, []int{}
	for s := 0; s < pat.Len; s++ {
		if pat.Swing[s] {
			swing = append(swing, s)
		}
		if pat.Slide[s] {
			slide = append(slide, s)
		}
	}
	used, total := e.WaveMemory()
	waves := []waveInfo{}
	for i := 0; i < engine.NumUserWave; i++ {
		if wv := e.Wave(i); wv != nil {
			waves = append(waves, waveInfo{i, wv.Name, wv.Rate, len(wv.Data)})
		}
	}
	songOn, song := e.SongMode()
	rows := []engine.SongRow{}
	loop := -1
	e.ReadSong(song, func(s *engine.Song) { rows = append(rows, s.Rows...); loop = s.LoopTo })
	cfg := a.config()
	return map[string]any{
		"playing": pos.Playing, "step": pos.Step, "pattern": pos.Pattern, "editPattern": e.CurrentPattern(),
		"patternName": engine.PatternName(pos.Pattern), "next": pos.Next,
		"tempo": e.InternalTempo(), "tempoNow": e.Tempo(), "record": e.Recording(), "sync": int(e.Sync()),
		"song": map[string]any{"on": songOn, "n": song, "row": pos.Row, "rows": rows, "loop": loop},
		"kit":  map[string]any{"n": e.KitNumber(), "name": e.KitName()},
		"pat": map[string]any{"len": pat.Len, "scale": pat.Scale, "swingAmt": pat.SwingAmt, "kit": pat.Kit,
			"poly": pat.Poly, "polyTrack": pat.PolyTrack, "trans": pat.Trans, "swing": swing, "slide": slide, "name": pat.Name},
		"grid": grid, "tracks": tracks, "master": master, "level": e.MasterPeak(),
		"audio": map[string]any{"ok": ok, "message": msg},
		"waves": waves, "waveUsed": used, "waveTotal": total,
		"imports": append([]string{}, a.st.ImportFiles()...),
		"config": map[string]any{"pcm_device": cfg.PCMDevice, "output_mode": cfg.OutputMode, "channel_offset": a.effectivePair(),
			"bus": a.busMode.Load(), "link": a.linkMode(), "midi_channel": cfg.MidiChannel},
	}
}

func machineList() []map[string]any {
	out := make([]map[string]any, len(engine.Machines))
	for i, m := range engine.Machines {
		out[i] = map[string]any{"id": i, "name": m.Name, "group": m.Group, "params": m.Params}
	}
	return out
}

func bad(w http.ResponseWriter, code int, err error) {
	http.Error(w, err.Error(), code)
}

func readJSON(r *http.Request, v any) error {
	return json.NewDecoder(io.LimitReader(r.Body, 1<<20)).Decode(v)
}

func ok(w http.ResponseWriter) { httpx.JSON(w, map[string]bool{"ok": true}) }

func slotOf(r *http.Request) (int, error) {
	n, err := strconv.Atoi(r.PathValue("slot"))
	if err != nil || n < 1 || n > engine.NumUserWave {
		return 0, errors.New("slot must be 1-32")
	}
	return n - 1, nil
}

func (w *webServer) routes() http.Handler {
	a := w.a
	e := a.eng
	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/state", func(rw http.ResponseWriter, r *http.Request) { httpx.JSON(rw, w.state()) })
	mux.HandleFunc("GET /api/machines", func(rw http.ResponseWriter, r *http.Request) {
		httpx.JSON(rw, map[string]any{"machines": machineList(), "pages": engine.PageNames, "master": engine.MasterNames,
			"scales": engine.ScaleNames, "arpModes": engine.ArpModeNames, "arpSpeeds": engine.ArpSpeedNames, "arpPlay": engine.ArpPlayNames,
			"names": paramNames()})
	})
	mux.HandleFunc("GET /sse/state", func(rw http.ResponseWriter, r *http.Request) {
		ch := w.broker.Register()
		w.clients.Add(1)
		defer func() { w.clients.Add(-1); w.broker.Unregister(ch) }()
		_ = sse.Serve(rw, r, ch, func(b []byte) ([]byte, error) { return b, nil })
	})
	mux.HandleFunc("POST /api/transport", func(rw http.ResponseWriter, r *http.Request) {
		var q struct {
			Action string  `json:"action"`
			Value  float64 `json:"value"`
		}
		if err := readJSON(r, &q); err != nil {
			bad(rw, 400, err)
			return
		}
		switch q.Action {
		case "play":
			e.Play()
		case "stop":
			e.Stop()
		case "toggle":
			e.TogglePlay()
		case "record":
			e.SetRecord(q.Value != 0)
		case "tempo":
			e.SetTempo(q.Value)
		case "sync":
			m := engine.SyncInternal
			if q.Value != 0 {
				m = engine.SyncMIDI
			}
			e.SetSync(m)
		default:
			bad(rw, 400, errors.New("unknown action"))
			return
		}
		ok(rw)
	})
	mux.HandleFunc("POST /api/note", func(rw http.ResponseWriter, r *http.Request) {
		var q struct {
			Track, Note int
			Vel         float64
			On          bool
		}
		if err := readJSON(r, &q); err != nil || q.Track < 0 || q.Track >= engine.NumTracks || q.Note < 0 || q.Note > 127 {
			bad(rw, 400, errors.New("bad note"))
			return
		}
		if q.Vel <= 0 || q.Vel > 1 {
			q.Vel = 0.8
		}
		if q.On {
			e.NoteOn(q.Track, q.Note, float32(q.Vel))
		} else {
			e.NoteOff(q.Track, q.Note)
		}
		ok(rw)
	})
	mux.HandleFunc("POST /api/param", func(rw http.ResponseWriter, r *http.Request) {
		var q struct{ Track, Param, Value int }
		if err := readJSON(r, &q); err != nil || q.Track < 0 || q.Track >= engine.NumTracks || q.Param < 0 || q.Param >= engine.NParams {
			bad(rw, 400, errors.New("bad parameter"))
			return
		}
		e.SetParam(q.Track, q.Param, q.Value)
		ok(rw)
	})
	mux.HandleFunc("POST /api/master", func(rw http.ResponseWriter, r *http.Request) {
		var q struct{ Index, Value int }
		if err := readJSON(r, &q); err != nil || q.Index < 0 || q.Index >= engine.NMaster {
			bad(rw, 400, errors.New("bad parameter"))
			return
		}
		e.SetMaster(q.Index, q.Value)
		ok(rw)
	})
	mux.HandleFunc("POST /api/machine", func(rw http.ResponseWriter, r *http.Request) {
		var q struct{ Track, Machine int }
		if err := readJSON(r, &q); err != nil || q.Track < 0 || q.Track >= engine.NumTracks || q.Machine < 0 || q.Machine >= len(engine.Machines) {
			bad(rw, 400, errors.New("bad machine"))
			return
		}
		e.SetMachine(q.Track, q.Machine)
		ok(rw)
	})
	mux.HandleFunc("POST /api/mute", func(rw http.ResponseWriter, r *http.Request) {
		var q struct {
			Track int
			Mute  bool
		}
		if err := readJSON(r, &q); err != nil || q.Track < 0 || q.Track >= engine.NumTracks {
			bad(rw, 400, errors.New("bad track"))
			return
		}
		e.SetMute(q.Track, q.Mute)
		ok(rw)
	})
	mux.HandleFunc("POST /api/step", func(rw http.ResponseWriter, r *http.Request) {
		var q struct {
			Track, Step int
			Action      string // toggle, note, add, off, clear, vel, len, trig, lock, unlock
			Value       int
		}
		if err := readJSON(r, &q); err != nil || q.Track < 0 || q.Track >= engine.NumTracks || q.Step < 0 || q.Step >= engine.MaxSteps {
			bad(rw, 400, errors.New("bad step"))
			return
		}
		switch q.Action {
		case "toggle":
			e.ToggleStep(q.Track, q.Step, q.Value)
		case "note":
			e.SetStepNote(q.Track, q.Step, q.Value, false)
		case "add":
			e.SetStepNote(q.Track, q.Step, q.Value, true)
		case "off":
			e.SetStepOff(q.Track, q.Step)
		case "clear":
			e.EditStep(q.Track, q.Step, func(s *engine.Step) { *s = engine.Step{} })
		case "vel":
			e.EditStep(q.Track, q.Step, func(s *engine.Step) { s.Vel = uint8(clamp(q.Value, 1, 127)) })
		case "len":
			e.EditStep(q.Track, q.Step, func(s *engine.Step) { s.Len = uint8(clamp(q.Value, 1, 127)) })
		case "trig":
			e.EditStep(q.Track, q.Step, func(s *engine.Step) { s.Trig = uint8(clamp(q.Value, 0, engine.TrigAll)) })
		case "unlock":
			e.ClearLock(q.Track, q.Step, q.Value)
		default:
			bad(rw, 400, errors.New("unknown action"))
			return
		}
		ok(rw)
	})
	mux.HandleFunc("POST /api/lock", func(rw http.ResponseWriter, r *http.Request) {
		var q struct{ Track, Step, Param, Value int }
		if err := readJSON(r, &q); err != nil || q.Param < 0 || q.Param >= engine.NParams {
			bad(rw, 400, errors.New("bad lock"))
			return
		}
		e.LockParam(q.Track, q.Step, q.Param, q.Value)
		ok(rw)
	})
	mux.HandleFunc("POST /api/layer", func(rw http.ResponseWriter, r *http.Request) {
		var q struct {
			Layer string
			Step  int
		}
		if err := readJSON(r, &q); err != nil || q.Step < 0 || q.Step >= engine.MaxSteps {
			bad(rw, 400, errors.New("bad layer"))
			return
		}
		switch q.Layer {
		case "swing":
			e.ToggleSwing(q.Step)
		case "slide":
			e.ToggleSlide(q.Step)
		default:
			bad(rw, 400, errors.New("layer is swing or slide"))
			return
		}
		ok(rw)
	})
	mux.HandleFunc("POST /api/patopt", func(rw http.ResponseWriter, r *http.Request) {
		var q struct {
			Len, Scale, SwingAmt, Kit, PolyTrack, Trans *int
			Poly                                        *bool
			Name                                        *string
		}
		if err := readJSON(r, &q); err != nil {
			bad(rw, 400, err)
			return
		}
		e.EditCurrent(func(p *engine.Pattern) {
			if q.Len != nil {
				p.Len = *q.Len
			}
			if q.Scale != nil {
				p.Scale = *q.Scale
			}
			if q.SwingAmt != nil {
				p.SwingAmt = *q.SwingAmt
			}
			if q.Kit != nil {
				p.Kit = *q.Kit
			}
			if q.PolyTrack != nil {
				p.PolyTrack = *q.PolyTrack
			}
			if q.Trans != nil {
				p.Trans = *q.Trans
			}
			if q.Poly != nil {
				p.Poly = *q.Poly
			}
			if q.Name != nil {
				p.Name = engine.CleanName(*q.Name, 12)
			}
		})
		ok(rw)
	})
	mux.HandleFunc("POST /api/edit", func(rw http.ResponseWriter, r *http.Request) {
		var q struct {
			Action      string
			Track, From int
			To, Value   int
		}
		if err := readJSON(r, &q); err != nil {
			bad(rw, 400, err)
			return
		}
		switch q.Action {
		case "clear-pattern":
			e.ClearPattern(q.From)
		case "copy-pattern":
			e.CopyPattern(q.From, q.To)
		case "clear-track":
			e.ClearTrack(q.Track)
		case "shift-track":
			e.ShiftTrack(q.Track, q.Value)
		case "transpose-track":
			e.TransposeTrack(q.Track, q.Value)
		default:
			bad(rw, 400, errors.New("unknown action"))
			return
		}
		ok(rw)
	})
	mux.HandleFunc("POST /api/track", func(rw http.ResponseWriter, r *http.Request) {
		var q struct {
			Track                  int
			KeyLP, KeyHP, TransFix *bool
			Trans, Src             *int
			Arp                    *engine.ArpSetup
		}
		if err := readJSON(r, &q); err != nil || q.Track < 0 || q.Track >= engine.NumTracks {
			bad(rw, 400, errors.New("bad track"))
			return
		}
		e.SetTrackOptions(q.Track, func(ts engine.TrackSetup) engine.TrackSetup {
			if q.KeyLP != nil {
				ts.KeyLP = *q.KeyLP
			}
			if q.KeyHP != nil {
				ts.KeyHP = *q.KeyHP
			}
			if q.TransFix != nil {
				ts.TransFix = *q.TransFix
			}
			if q.Trans != nil {
				ts.Trans = *q.Trans
			}
			if q.Src != nil {
				ts.Src = uint8(*q.Src)
			}
			if q.Arp != nil {
				ts.Arp = *q.Arp
			}
			return ts
		})
		ok(rw)
	})
	mux.HandleFunc("POST /api/song", func(rw http.ResponseWriter, r *http.Request) {
		var q struct {
			N      int
			On     *bool
			Rows   *[]engine.SongRow
			LoopTo *int
		}
		if err := readJSON(r, &q); err != nil || q.N < 0 || q.N >= engine.NumSongs {
			bad(rw, 400, errors.New("song 0-23"))
			return
		}
		if q.Rows != nil || q.LoopTo != nil {
			e.EditSong(q.N, func(s *engine.Song) {
				if q.Rows != nil {
					s.Rows = append([]engine.SongRow(nil), *q.Rows...)
				}
				if q.LoopTo != nil {
					s.LoopTo = *q.LoopTo
				}
			})
		}
		if q.On != nil {
			e.SetSong(*q.On, q.N)
		}
		ok(rw)
	})
	mux.HandleFunc("POST /api/pattern", func(rw http.ResponseWriter, r *http.Request) {
		var q struct {
			N   int
			Now bool
		}
		if err := readJSON(r, &q); err != nil {
			bad(rw, 400, err)
			return
		}
		if q.N < 0 || q.N >= engine.NumPatterns {
			bad(rw, 400, errors.New("pattern 0-127"))
			return
		}
		e.SelectPattern(q.N, q.Now)
		ok(rw)
	})
	mux.HandleFunc("POST /api/kit", func(rw http.ResponseWriter, r *http.Request) {
		var q struct {
			Action string
			N      int
			Name   string
		}
		if err := readJSON(r, &q); err != nil || q.N < 0 || q.N >= engine.NumKits {
			bad(rw, 400, errors.New("kit 0-127"))
			return
		}
		switch q.Action {
		case "load":
			e.LoadKit(q.N)
		case "save":
			e.SaveKit(q.N, q.Name)
		case "reset":
			e.ResetKit()
		default:
			bad(rw, 400, errors.New("unknown action"))
			return
		}
		ok(rw)
	})
	// UserWave: PUT a WAV body (?name=file.wav) or choose one from imports.
	mux.HandleFunc("PUT /api/wave/{slot}", func(rw http.ResponseWriter, r *http.Request) {
		slot, err := slotOf(r)
		if err != nil {
			bad(rw, 400, err)
			return
		}
		name := r.URL.Query().Get("name")
		if name == "" {
			name = fmt.Sprintf("UW%02d.wav", slot+1)
		}
		if err := a.st.UploadWave(slot, name, r.Body); err != nil {
			bad(rw, 422, err)
			return
		}
		ok(rw)
	})
	mux.HandleFunc("POST /api/wave/{slot}/import", func(rw http.ResponseWriter, r *http.Request) {
		slot, err := slotOf(r)
		if err != nil {
			bad(rw, 400, err)
			return
		}
		if err := a.st.ImportWave(slot, r.URL.Query().Get("file")); err != nil {
			bad(rw, 422, err)
			return
		}
		ok(rw)
	})
	mux.HandleFunc("DELETE /api/wave/{slot}", func(rw http.ResponseWriter, r *http.Request) {
		slot, err := slotOf(r)
		if err != nil {
			bad(rw, 400, err)
			return
		}
		e.ClearWave(slot)
		ok(rw)
	})
	mux.HandleFunc("GET /api/wave/{slot}", func(rw http.ResponseWriter, r *http.Request) {
		slot, err := slotOf(r)
		if err != nil {
			bad(rw, 400, err)
			return
		}
		wv := e.Wave(slot)
		if wv == nil {
			http.NotFound(rw, r)
			return
		}
		rw.Header().Set("Content-Type", "audio/wav")
		_, _ = rw.Write(engine.EncodeWAV16(wv.Data, wv.Rate, 1))
	})
	mux.HandleFunc("GET /api/project", func(rw http.ResponseWriter, r *http.Request) {
		b, err := a.st.ProjectJSON()
		if err != nil {
			bad(rw, 500, err)
			return
		}
		rw.Header().Set("Content-Type", "application/json")
		rw.Header().Set("Content-Disposition", `attachment; filename="monomodule-project.json"`)
		_, _ = rw.Write(b)
	})
	mux.HandleFunc("POST /api/project", func(rw http.ResponseWriter, r *http.Request) {
		b, err := io.ReadAll(io.LimitReader(r.Body, 32<<20))
		if err != nil {
			bad(rw, 400, err)
			return
		}
		if err := a.st.ReplaceProject(b); err != nil {
			bad(rw, 422, err)
			return
		}
		ok(rw)
	})
	mux.HandleFunc("POST /api/config", func(rw http.ResponseWriter, r *http.Request) {
		var q struct {
			OutputMode    *string `json:"output_mode"`
			ChannelOffset *int    `json:"channel_offset"`
			MidiChannel   *int    `json:"midi_channel"`
			PCMDevice     *string `json:"pcm_device"`
		}
		if err := readJSON(r, &q); err != nil {
			bad(rw, 400, err)
			return
		}
		cur := a.config()
		mono := cur.OutputMode == outmix.Mono
		if q.OutputMode != nil {
			if !outmix.Valid(*q.OutputMode) {
				bad(rw, 400, errors.New("unknown output mode"))
				return
			}
			mono = *q.OutputMode == outmix.Mono
		}
		devices, _ := alsapcm.EnumPlaybackDevices()
		if q.PCMDevice != nil && !audioout.HasDevice(devices, *q.PCMDevice) {
			bad(rw, 400, errors.New("unknown audio device"))
			return
		}
		// The mixer plays stereo pairs only; the mono choice is for the direct path.
		if q.ChannelOffset != nil && !audioout.ValidChannel(int(a.channels.Load()), mono && !a.busMode.Load(), *q.ChannelOffset) {
			bad(rw, 400, errors.New("channel not on this card"))
			return
		}
		err := a.setConfig(func(c *store.Config) {
			if q.OutputMode != nil {
				c.OutputMode = *q.OutputMode
			}
			if q.ChannelOffset != nil {
				c.ChannelOffset = *q.ChannelOffset
				if !mono {
					c.BusPair = *q.ChannelOffset
				}
			}
			if q.MidiChannel != nil && *q.MidiChannel >= -1 && *q.MidiChannel <= 15 {
				c.MidiChannel = *q.MidiChannel
			}
			if q.PCMDevice != nil {
				c.PCMDevice = *q.PCMDevice
			}
		})
		if err != nil {
			bad(rw, 500, err)
			return
		}
		if c := a.busClient.Load(); c != nil && q.ChannelOffset != nil && !mono {
			c.SetPair(*q.ChannelOffset)
		}
		ok(rw)
	})
	// The audio outputs on offer. Read on demand: listing devices reads /proc,
	// too much for the 100 ms state broadcast.
	mux.HandleFunc("GET /api/audio", func(rw http.ResponseWriter, r *http.Request) {
		devices, _ := alsapcm.EnumPlaybackDevices()
		cfg := a.config()
		bus := a.busMode.Load()
		mode := cfg.OutputMode
		if m := r.URL.Query().Get("mode"); outmix.Valid(m) {
			mode = m // the page asks about a mode it has not applied yet
		}
		mono := mode == outmix.Mono && !bus
		httpx.JSON(rw, map[string]any{
			"devices":  audioout.Devices(devices, cfg.PCMDevice),
			"channels": audioout.Channels(int(a.channels.Load()), mono, a.effectivePair()),
			"bus":      bus,
			"link":     a.linkMode(),
		})
	})
	mux.HandleFunc("POST /api/live/tracks", func(rw http.ResponseWriter, r *http.Request) {
		var res livelink.Result
		var err error
		if a.linkMode() {
			res, err = livelink.EnsureMIDITrack(livelink.Call)
		} else {
			mono := a.config().OutputMode == outmix.Mono && !a.busMode.Load()
			res, err = livelink.EnsureTracks(livelink.Call, a.effectivePair(), mono)
		}
		if err != nil {
			bad(rw, 502, err)
			return
		}
		httpx.JSON(rw, res)
	})
	mux.HandleFunc("/", httpx.ServeEmbedded(webFS, "web/index.html"))
	return httpx.WithLogging(httpx.WithCORS("GET, POST, PUT, DELETE, OPTIONS", mux))
}

func (a *app) runWeb(port int, shutdown <-chan struct{}) {
	w := &webServer{a: a, broker: sse.NewBroker[[]byte](8, false)}
	srv := httpx.NewServer(fmt.Sprintf(":%d", port), w.routes())
	go func() {
		t := time.NewTicker(100 * time.Millisecond)
		defer t.Stop()
		for {
			select {
			case <-shutdown:
				return
			case <-t.C:
				if w.clients.Load() == 0 {
					continue
				}
				if b, err := json.Marshal(w.state()); err == nil {
					w.broker.Broadcast(b)
				}
			}
		}
	}()
	go func() {
		<-shutdown
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = srv.Shutdown(ctx)
	}()
	log.Printf("web UI on %s", srv.Addr)
	if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
		log.Printf("web server: %v", err)
	}
}

func clamp(v, lo, hi int) int {
	if v < lo {
		return lo
	}
	if v > hi {
		return hi
	}
	return v
}

// paramNames lists, for every machine, the 56 parameter names.
func paramNames() [][]string {
	out := make([][]string, len(engine.Machines))
	for m := range out {
		out[m] = make([]string, engine.NParams)
		for i := range out[m] {
			out[m][i] = engine.ParamName(m, i)
		}
	}
	return out
}

func ints(b []uint8) []int {
	o := make([]int, len(b))
	for i, v := range b {
		o[i] = int(v)
	}
	return o
}
