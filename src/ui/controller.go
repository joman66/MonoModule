// Package ui is MonoModule's front panel: it turns Push 3 pads, buttons and
// encoders into Monomachine-style operations, draws the screen and decides
// what the LEDs show. It does no I/O of its own (the host moves the events
// and the frames), so all of it can be tested on any computer.
package ui

import (
	"fmt"
	"sync"
	"time"

	"monomodule/engine"

	"github.com/federico-pepe/ableton-push-hack/core/push3"
)

// Pages. The first eight are on the buttons above the screen; SYSTEM is on
// the Settings button.
const (
	PgSyn = iota
	PgAmp
	PgFlt
	PgFx
	PgLfo
	PgSeq
	PgKit
	PgSong
	PgSys
	numPages
)

var pageNames = [numPages]string{"SYN", "AMP", "FLT", "FX", "LFO", "SEQ", "KIT", "SONG", "SYSTEM"}

// PageName is the label of a page.
func PageName(p int) string {
	if p < 0 || p >= numPages {
		return ""
	}
	return pageNames[p]
}

type layerKind int

const (
	layerNone layerKind = iota
	layerSwing
	layerSlide
)

// Host is what the front panel needs from the program around it.
type Host interface {
	ImportFiles() []string
	ImportWave(slot int, file string) error
	SaveProject() error
	AudioStatus() (ok bool, msg string)
	SyncChanged(m engine.SyncMode)
}

type stepHold struct {
	existed, edited, noted bool
}

type overlayKind int

const (
	ovNone overlayKind = iota
	ovMachine
)

// Controller is the state of the front panel.
type Controller struct {
	mu   sync.Mutex
	eng  *engine.Engine
	host Host
	now  func() time.Time

	page, track, sub  int
	stepPage, bank    int
	octave            int
	layer             layerKind
	muteMode          bool
	soloHeld          bool
	shift, cpy, clear bool
	held              map[int]*stepHold
	sounding          map[byte]int // pad note -> note being played
	ov                overlayKind
	ovCursor          int
	toast             string
	toastUntil        time.Time
	taps              []time.Time
	songRow, songNum  int
	kitCursor         int
	srcCursor         int
	waveSlot, fileCur int
	clipPat           int
	acc               [8]int
	lastTouch         int
	lastTouchAt       time.Time
}

// New makes the panel for an engine.
func New(e *engine.Engine, h Host) *Controller {
	return &Controller{eng: e, host: h, now: time.Now, held: map[int]*stepHold{}, sounding: map[byte]int{},
		clipPat: -1, lastTouch: -1, kitCursor: e.KitNumber()}
}

// SetClock replaces the time source (for tests).
func (c *Controller) SetClock(f func() time.Time) { c.now = f }

// Page is the page showing now.
func (c *Controller) Page() int { c.mu.Lock(); defer c.mu.Unlock(); return c.page }

// SetView jumps to a page and sub page (for screenshots and tests).
func (c *Controller) SetView(page, sub int) {
	c.mu.Lock()
	c.page, c.sub, c.ov = page, sub, ovNone
	c.mu.Unlock()
}

// Track is the selected track (0..5).
func (c *Controller) Track() int { c.mu.Lock(); defer c.mu.Unlock(); return c.track }

func (c *Controller) say(format string, a ...any) {
	c.toast = fmt.Sprintf(format, a...)
	c.toastUntil = c.now().Add(1500 * time.Millisecond)
}

func clampi(v, lo, hi int) int {
	if v < lo {
		return lo
	}
	if v > hi {
		return hi
	}
	return v
}

func (c *Controller) ticks(i, delta, n int) int {
	c.acc[i] += delta
	s := c.acc[i] / n
	c.acc[i] -= s * n
	return s
}

// pattern is a copy of the current pattern.
func (c *Controller) pattern() engine.Pattern {
	var p engine.Pattern
	c.eng.ReadCurrent(func(x *engine.Pattern) { p = *x })
	return p
}

// paramIndex is the kit parameter encoder i controls on the page, or -1.
func (c *Controller) paramIndex(i int) int {
	switch c.page {
	case PgSyn, PgAmp, PgFlt, PgFx:
		return c.page*engine.PerPage + i
	case PgLfo:
		return engine.LFOBase(c.sub) + i
	}
	return -1
}

func (c *Controller) stepAbs(note byte) int {
	col, row := push3.PadCoord(note)
	return c.stepPage*16 + (1-row)*8 + col // row 1 holds steps 1-8, row 0 holds 9-16
}

// baseNote is the note of the lowest pitch pad.
func (c *Controller) baseNote() int { return 48 + 12*c.octave }

// ------------------------------------------------------------- pads

// Pad handles a pad press (vel > 0) or release (vel == 0).
func (c *Controller) Pad(note byte, vel byte) {
	if !push3.IsPadNote(note) {
		return
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	col, row := push3.PadCoord(note)
	down := vel > 0
	v := float32(vel) / 127
	if v < 0.35 {
		v = 0.35
	}
	if row >= 5 { // pitch keys
		c.pitchKey(note, col, row, down, v)
		return
	}
	switch {
	case row <= 1:
		pat := c.pattern()
		s := c.stepAbs(note)
		if s >= pat.Len {
			return
		}
		if down {
			c.stepDown(s, pat)
		} else {
			c.stepUp(s)
		}
	case !down:
		return
	case row == 2:
		if col < engine.NumTracks {
			c.trackKey(col)
		}
	default: // rows 3-4: patterns
		c.patternKey(c.bank*16 + (4-row)*8 + col)
	}
}

func (c *Controller) pitchKey(note byte, col, row int, down bool, v float32) {
	if !down {
		if n, ok := c.sounding[note]; ok {
			delete(c.sounding, note)
			c.eng.NoteOff(c.track, n)
		}
		return
	}
	n := clampi(c.baseNote()+(row-5)*8+col, 0, 127)
	if len(c.held) > 0 { // a held step takes the note
		pat := c.pattern()
		for s, h := range c.held {
			if s >= pat.Len {
				continue
			}
			c.eng.SetStepNote(c.track, s, n, h.noted)
			h.noted, h.edited = true, true
		}
	}
	c.sounding[note] = n
	c.eng.NoteOn(c.track, n, v)
}

func (c *Controller) stepDown(s int, pat engine.Pattern) {
	switch {
	case c.shift:
		c.eng.EditCurrent(func(p *engine.Pattern) { p.Len = s + 1 })
		c.say("LENGTH %d", s+1)
	case c.layer == layerSwing:
		c.eng.ToggleSwing(s)
	case c.layer == layerSlide:
		c.eng.ToggleSlide(s)
	case c.clear:
		c.eng.EditStep(c.track, s, func(st *engine.Step) { *st = engine.Step{} })
	default:
		existed := pat.Tracks[c.track].Step[s].On
		if !existed {
			c.eng.ToggleStep(c.track, s, c.lastNote(pat))
		}
		c.held[s] = &stepHold{existed: existed}
	}
}

// lastNote is the note a new trig takes: the one of the nearest earlier trig
// of the track, else middle C of the keyboard.
func (c *Controller) lastNote(pat engine.Pattern) int {
	for s := pat.Len - 1; s >= 0; s-- {
		if st := pat.Tracks[c.track].Step[s]; st.On && !st.Off {
			return int(st.Note)
		}
	}
	return engine.DefaultNote
}

func (c *Controller) stepUp(s int) {
	h := c.held[s]
	if h == nil {
		return
	}
	delete(c.held, s)
	if h.existed && !h.edited {
		c.eng.EditStep(c.track, s, func(st *engine.Step) { *st = engine.Step{} })
	}
}

func (c *Controller) trackKey(t int) {
	switch {
	case c.muteMode:
		c.eng.SetMute(t, !c.eng.Muted(t))
	case c.soloHeld:
		solo := true
		for i := 0; i < engine.NumTracks; i++ {
			if i != t && !c.eng.Muted(i) {
				solo = false
			}
		}
		for i := 0; i < engine.NumTracks; i++ {
			c.eng.SetMute(i, !solo && i != t)
		}
	case c.clear:
		c.eng.ClearTrack(t)
		c.say("%s CLEARED", engine.TrackNames[t])
	default:
		c.track = t
	}
}

func (c *Controller) patternKey(p int) {
	switch {
	case c.clear:
		c.eng.ClearPattern(p)
		c.say("%s CLEARED", engine.PatternName(p))
	case c.cpy:
		cur := c.eng.CurrentPattern()
		c.eng.CopyPattern(cur, p)
		c.say("%s TO %s", engine.PatternName(cur), engine.PatternName(p))
	default:
		c.eng.SelectPattern(p, c.shift)
	}
}

// ---------------------------------------------------------- buttons

// Button handles a button press (down) or release.
func (c *Controller) Button(cc byte, down bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	switch cc {
	case push3.CCShift:
		c.shift = down
		return
	case push3.CCDuplicate:
		c.cpy = down
		if down {
			c.clipPat = c.eng.CurrentPattern()
			c.say("COPY: PRESS A PATTERN")
		}
		return
	case push3.CCDelete:
		c.clear = down
		return
	case push3.CCSolo:
		c.soloHeld = down
		return
	}
	if !down {
		return
	}
	switch {
	case cc >= push3.CCScreenTop1 && cc <= push3.CCScreenTop8:
		c.setPage(int(cc - push3.CCScreenTop1))
	case cc >= push3.CCScreenBot1 && cc <= push3.CCScreenBot8:
		c.softKey(int(cc - push3.CCScreenBot1))
	case cc >= push3.CCScene14 && cc <= push3.CCScene132t:
		c.bank = 7 - int(cc-push3.CCScene14)
	case cc == push3.CCPlay:
		if !c.eng.LocalTransport() {
			c.say("PLAY FOLLOWS LIVE (SYNC MIDI)")
		} else if c.shift {
			c.eng.Stop()
			c.eng.Play()
		} else {
			c.eng.TogglePlay()
		}
	case cc == push3.CCRecord:
		c.eng.SetRecord(!c.eng.Recording())
		c.say("REC %s", onOff(c.eng.Recording()))
	case cc == push3.CCTapTempo:
		c.tap()
	case cc == push3.CCMute:
		c.muteMode = !c.muteMode
	case cc == push3.CCQuantize:
		c.toggleLayer(layerSwing)
	case cc == push3.CCRepeat:
		c.toggleLayer(layerSlide)
	case cc == push3.CCConvert:
		if c.clipPat >= 0 {
			cur := c.eng.CurrentPattern()
			c.eng.CopyPattern(c.clipPat, cur)
			c.say("PASTED %s", engine.PatternName(c.clipPat))
		}
	case cc == push3.CCSave:
		c.save()
	case cc == push3.CCSettings:
		c.setPage(PgSys)
	case cc == push3.CCUndo:
		c.ov = ovNone
	case cc == push3.CCJogPress || cc == push3.CCDPadCenter:
		c.enter()
	case cc == push3.CCDPadLeft:
		c.track = (c.track + engine.NumTracks - 1) % engine.NumTracks
	case cc == push3.CCDPadRight:
		c.track = (c.track + 1) % engine.NumTracks
	case cc == push3.CCDPadUp:
		c.eng.SelectPattern(c.eng.CurrentPattern()+1, c.shift)
	case cc == push3.CCDPadDown:
		c.eng.SelectPattern(c.eng.CurrentPattern()-1, c.shift)
	case cc == push3.CCPageLeft:
		c.stepPage = clampi(c.stepPage-1, 0, 3)
	case cc == push3.CCPageRight:
		c.stepPage = clampi(c.stepPage+1, 0, (c.pattern().Len-1)/16)
	case cc == push3.CCOctaveUp:
		c.octave = clampi(c.octave+1, -3, 3)
		c.say("OCTAVE %+d", c.octave)
	case cc == push3.CCOctaveDown:
		c.octave = clampi(c.octave-1, -3, 3)
		c.say("OCTAVE %+d", c.octave)
	}
}

func onOff(b bool) string {
	if b {
		return "ON"
	}
	return "OFF"
}

func subCount(p int) int {
	switch p {
	case PgLfo:
		return 3
	case PgSeq, PgKit:
		return 2
	}
	return 1
}

func (c *Controller) setPage(p int) {
	if p == c.page {
		c.sub = (c.sub + 1) % subCount(p)
	} else {
		c.sub = 0
	}
	c.page = p
	c.ov = ovNone
	c.acc = [8]int{}
}

func (c *Controller) toggleLayer(l layerKind) {
	if c.layer == l {
		c.layer = layerNone
	} else {
		c.layer = l
	}
}

func (c *Controller) save() {
	if err := c.host.SaveProject(); err != nil {
		c.say("SAVE FAILED")
		return
	}
	c.say("PROJECT SAVED")
}

func (c *Controller) tap() {
	now := c.now()
	if n := len(c.taps); n > 0 && now.Sub(c.taps[n-1]) > 2*time.Second {
		c.taps = nil
	}
	c.taps = append(c.taps, now)
	if len(c.taps) > 5 {
		c.taps = c.taps[1:]
	}
	if len(c.taps) >= 2 {
		span := c.taps[len(c.taps)-1].Sub(c.taps[0]).Seconds() / float64(len(c.taps)-1)
		if span > 0 {
			c.eng.SetTempo(60 / span)
			c.say("TEMPO %.1f", c.eng.Tempo())
		}
	}
}

func (c *Controller) enter() {
	if c.ov == ovMachine {
		c.eng.SetMachine(c.track, c.ovCursor)
		c.say("%s", engine.Machines[c.ovCursor].Name)
		c.ov = ovNone
	}
}

func (c *Controller) prevTrack() { c.track = (c.track + engine.NumTracks - 1) % engine.NumTracks }
func (c *Controller) nextTrack() { c.track = (c.track + 1) % engine.NumTracks }

// softKey runs bottom button i of the current page.
func (c *Controller) softKey(i int) {
	switch c.page {
	case PgSyn:
		switch i {
		case 0:
			if c.ov == ovMachine {
				c.enter()
			} else {
				c.ov = ovMachine
				c.ovCursor = c.eng.Machine(c.track)
			}
		case 1:
			c.prevTrack()
		case 2:
			c.nextTrack()
		}
	case PgAmp, PgFlt, PgFx, PgLfo:
		switch i {
		case 1:
			c.prevTrack()
		case 2:
			c.nextTrack()
		case 7:
			if c.held != nil && len(c.held) > 0 {
				for s := range c.held {
					c.eng.ClearLock(c.track, s, -1)
				}
				c.say("LOCKS CLEARED")
			}
		}
	case PgSeq:
		cur := c.eng.CurrentPattern()
		switch i {
		case 0:
			if c.shift {
				c.eng.ClearPattern(cur)
				c.say("PATTERN CLEARED")
			}
		case 1:
			c.clipPat = cur
			c.say("COPIED %s", engine.PatternName(cur))
		case 2:
			if c.clipPat >= 0 {
				c.eng.CopyPattern(c.clipPat, cur)
				c.say("PASTED %s", engine.PatternName(c.clipPat))
			}
		case 3:
			c.eng.SetRecord(!c.eng.Recording())
		case 4:
			c.eng.ShiftTrack(c.track, -1)
		case 5:
			c.eng.ShiftTrack(c.track, 1)
		case 6:
			c.eng.ClearTrack(c.track)
			c.say("%s CLEARED", engine.TrackNames[c.track])
		}
	case PgKit:
		switch i {
		case 0:
			c.eng.LoadKit(c.kitCursor)
			c.say("KIT %03d LOADED", c.kitCursor+1)
		case 1:
			c.eng.SaveKit(c.kitCursor, "")
			c.say("KIT %03d SAVED", c.kitCursor+1)
		case 2:
			c.eng.ResetKit()
			c.say("KIT INIT")
		}
	case PgSong:
		var rows int
		c.eng.ReadSong(c.songNum, func(s *engine.Song) { rows = len(s.Rows) })
		switch i {
		case 0:
			cur := c.eng.CurrentPattern()
			c.eng.EditSong(c.songNum, func(s *engine.Song) {
				s.Rows = append(s.Rows, engine.SongRow{Pattern: cur, Repeats: 1})
			})
			c.songRow = rows
		case 1:
			c.eng.EditSong(c.songNum, func(s *engine.Song) {
				if c.songRow < len(s.Rows) {
					s.Rows = append(s.Rows[:c.songRow], s.Rows[c.songRow+1:]...)
				}
			})
			c.songRow = clampi(c.songRow, 0, rows-1)
		case 2:
			on, _ := c.eng.SongMode()
			c.eng.SetSong(!on, c.songNum)
			c.say("SONG MODE %s", onOff(!on))
		}
	case PgSys:
		files := c.host.ImportFiles()
		switch i {
		case 0:
			if len(files) == 0 {
				c.say("NO WAV FILES")
				return
			}
			c.fileCur = clampi(c.fileCur, 0, len(files)-1)
			if err := c.host.ImportWave(c.waveSlot, files[c.fileCur]); err != nil {
				c.say("%s", engine.CleanName(err.Error(), 28))
			} else {
				c.say("UW-%02d LOADED", c.waveSlot+1)
			}
		case 1:
			c.eng.ClearWave(c.waveSlot)
			c.say("UW-%02d CLEARED", c.waveSlot+1)
		case 2:
			c.save()
		}
	}
}

// ---------------------------------------------------------- encoders

// Encoder handles one encoder turn. cc is the encoder CC; delta is signed.
func (c *Controller) Encoder(cc byte, delta int) {
	if delta == 0 {
		return
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	switch {
	case cc >= push3.CCEncoder1 && cc <= push3.CCEncoder8:
		c.knob(int(cc-push3.CCEncoder1), delta)
	case cc == push3.CCJogWheel:
		c.jog(delta)
	case cc == push3.CCVolume:
		c.eng.SetMaster(engine.MVOL, c.eng.Master(engine.MVOL)+delta)
		c.say("MASTER %d", c.eng.Master(engine.MVOL))
	case cc == push3.CCTempo:
		d := float64(delta)
		if c.shift {
			d /= 10
		}
		c.eng.SetTempo(c.eng.InternalTempo() + d)
	}
}

func (c *Controller) jog(delta int) {
	switch {
	case c.ov == ovMachine:
		c.ovCursor = clampi(c.ovCursor+delta, 0, len(engine.Machines)-1)
	case c.page == PgKit:
		c.kitCursor = clampi(c.kitCursor+delta, 0, engine.NumKits-1)
	case c.page == PgSong:
		var rows int
		c.eng.ReadSong(c.songNum, func(s *engine.Song) { rows = len(s.Rows) })
		c.songRow = clampi(c.songRow+delta, 0, rows)
	case c.page == PgSys:
		c.fileCur += delta
		if n := len(c.host.ImportFiles()); n > 0 {
			c.fileCur = clampi(c.fileCur, 0, n-1)
		} else {
			c.fileCur = 0
		}
	default:
		c.eng.SetParam(c.track, engine.VOL, c.eng.Param(c.track, engine.VOL)+delta)
	}
}

func (c *Controller) knob(i, delta int) {
	c.lastTouch, c.lastTouchAt = i, c.now()
	if c.ov == ovMachine {
		if i == 0 {
			c.ovCursor = clampi(c.ovCursor+delta, 0, len(engine.Machines)-1)
		}
		return
	}
	switch c.page {
	case PgSyn, PgAmp, PgFlt, PgFx, PgLfo:
		p := c.paramIndex(i)
		edited := false
		var pat engine.Pattern
		if len(c.held) > 0 {
			pat = c.pattern()
		}
		for s, h := range c.held {
			if s >= pat.Len || !pat.Tracks[c.track].Step[s].On {
				continue
			}
			cur := c.eng.Param(c.track, p)
			if l, ok := c.eng.StepLock(c.track, s); ok && l.Has(p) {
				cur = int(l.Val[p])
			}
			c.eng.LockParam(c.track, s, p, cur+delta)
			h.edited, edited = true, true
		}
		if !edited {
			c.eng.SetParam(c.track, p, c.eng.Param(c.track, p)+delta)
		}
	case PgSeq:
		if len(c.held) > 0 && c.stepKnob(i, delta) {
			return
		}
		if c.sub == 0 {
			c.seqKnob(i, delta)
		} else {
			c.arpKnob(i, delta)
		}
	case PgKit:
		if c.sub == 0 {
			c.kitKnob(i, delta)
		} else {
			c.eng.SetMaster(i, c.eng.Master(i)+delta)
		}
	case PgSong:
		c.songKnob(i, delta)
	case PgSys:
		switch i {
		case 0:
			c.waveSlot = clampi(c.waveSlot+c.ticks(i, delta, 1), 0, engine.NumUserWave-1)
		case 1:
			c.jog(delta)
		case 6:
			if s := c.ticks(i, delta, 6); s != 0 {
				m := engine.SyncInternal
				if s > 0 {
					m = engine.SyncMIDI
				}
				c.eng.SetSync(m)
				c.host.SyncChanged(m)
			}
		}
	}
}

// stepKnob edits the held steps: note, velocity, length, trig. It reports
// whether encoder i did something.
func (c *Controller) stepKnob(i, delta int) bool {
	if i > 3 {
		return false
	}
	for s, h := range c.held {
		c.eng.EditStep(c.track, s, func(st *engine.Step) {
			if !st.On {
				return
			}
			switch i {
			case 0:
				st.Note = uint8(clampi(int(st.Note)+delta, 0, 127))
			case 1:
				st.Vel = uint8(clampi(int(st.Vel)+delta, 1, 127))
			case 2:
				st.Len = uint8(clampi(int(st.Len)+delta, 1, 127))
			case 3:
				if d := c.ticks(i, delta, 4); d != 0 {
					st.Trig = uint8(clampi(int(st.Trig)+d, 0, engine.TrigAll))
				}
			}
		})
		h.edited = true
	}
	return true
}

func (c *Controller) seqKnob(i, delta int) {
	p := c.pattern()
	switch i {
	case 0:
		c.eng.EditCurrent(func(x *engine.Pattern) { x.Len = clampi(p.Len+delta, 2, engine.MaxSteps) })
	case 1:
		if d := c.ticks(i, delta, 4); d != 0 {
			c.eng.EditCurrent(func(x *engine.Pattern) { x.Scale = clampi(p.Scale+d, 0, len(engine.ScaleNames)-1) })
		}
	case 2:
		c.eng.EditCurrent(func(x *engine.Pattern) { x.SwingAmt = clampi(p.SwingAmt+delta, 50, 80) })
	case 3:
		if d := c.ticks(i, delta, 3); d != 0 {
			c.eng.EditCurrent(func(x *engine.Pattern) { x.Kit = clampi(p.Kit+d, -1, engine.NumKits-1) })
		}
	case 4:
		if d := c.ticks(i, delta, 2); d != 0 {
			c.eng.EditCurrent(func(x *engine.Pattern) { x.Trans = clampi(p.Trans+d, -24, 24) })
		}
	case 5:
		if d := c.ticks(i, delta, 6); d != 0 {
			c.eng.EditCurrent(func(x *engine.Pattern) { x.Poly = d > 0; x.PolyTrack = c.track })
		}
	case 6:
		c.eng.EditCurrent(func(x *engine.Pattern) {
			if d := c.ticks(i, delta, 3); d != 0 {
				x.PolyTrack = clampi(p.PolyTrack+d, 0, engine.NumTracks-1)
			}
		})
	case 7:
		d := float64(delta)
		if c.shift {
			d /= 10
		}
		c.eng.SetTempo(c.eng.InternalTempo() + d)
	}
}

func (c *Controller) arpKnob(i, delta int) {
	t := c.track
	c.eng.SetTrackOptions(t, func(ts engine.TrackSetup) engine.TrackSetup {
		a := &ts.Arp
		switch i {
		case 0:
			a.Mode += c.ticks(i, delta, 4)
		case 1:
			a.Speed += c.ticks(i, delta, 4)
		case 2:
			a.Range += c.ticks(i, delta, 4)
		case 3:
			a.Len += delta
		case 4:
			a.Play += c.ticks(i, delta, 6)
		case 5:
			ts.Trans += c.ticks(i, delta, 2)
		case 6:
			if d := c.ticks(i, delta, 6); d != 0 {
				ts.TransFix = d > 0
			}
		}
		return ts
	})
	if i == 7 {
		c.eng.SetTempo(c.eng.InternalTempo() + float64(delta))
	}
}

func (c *Controller) kitKnob(i, delta int) {
	switch i {
	case 0:
		c.kitCursor = clampi(c.kitCursor+delta, 0, engine.NumKits-1)
	case 1, 2:
		if d := c.ticks(i, delta, 6); d != 0 {
			c.eng.SetTrackOptions(c.track, func(ts engine.TrackSetup) engine.TrackSetup {
				if i == 1 {
					ts.KeyHP = d > 0
				} else {
					ts.KeyLP = d > 0
				}
				return ts
			})
		}
	case 3:
		c.srcCursor = clampi(c.srcCursor+c.ticks(i, delta, 3), 0, engine.NumTracks-1)
	case 4:
		if d := c.ticks(i, delta, 6); d != 0 {
			bit := uint8(1) << uint(c.srcCursor)
			c.eng.SetTrackOptions(c.track, func(ts engine.TrackSetup) engine.TrackSetup {
				if d > 0 {
					ts.Src |= bit
				} else {
					ts.Src &^= bit
				}
				return ts
			})
		}
	}
}

func (c *Controller) songKnob(i, delta int) {
	var s engine.Song
	c.eng.ReadSong(c.songNum, func(x *engine.Song) { s = *x; s.Rows = append([]engine.SongRow(nil), x.Rows...) })
	switch i {
	case 0:
		c.songRow = clampi(c.songRow+c.ticks(i, delta, 2), 0, len(s.Rows))
	case 1, 2, 3:
		if c.songRow >= len(s.Rows) && i != 3 {
			return
		}
		d := c.ticks(i, delta, 2)
		c.eng.EditSong(c.songNum, func(x *engine.Song) {
			switch i {
			case 1:
				x.Rows[c.songRow].Pattern += d
			case 2:
				x.Rows[c.songRow].Repeats += d
			case 3:
				x.LoopTo = clampi(x.LoopTo+d, -1, len(x.Rows)-1)
			}
		})
	case 4:
		on, _ := c.eng.SongMode()
		c.songNum = clampi(c.songNum+c.ticks(i, delta, 4), 0, engine.NumSongs-1)
		if on {
			c.eng.SetSong(true, c.songNum)
		}
		c.songRow = 0
	case 5:
		c.eng.EditSong(c.songNum, func(x *engine.Song) {
			if c.songRow < len(x.Rows) {
				x.Rows[c.songRow].Trans += c.ticks(i, delta, 2)
			}
		})
	}
}
