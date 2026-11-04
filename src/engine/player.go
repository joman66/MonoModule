package engine

import (
	"math"
	"time"
)

// ---------------------------------------------------------- the clock

// fireTime is when step k of pat fires, in ticks from the start of the
// pattern. Step Len is the end of the pattern. A swung step is late by a
// fraction of a step: 50% is straight, 66% is triplet swing, 75% is half a
// step late.
func (e *Engine) fireTime(pat *Pattern, k int) float64 {
	st := float64(StepTicks(pat.Scale))
	if k >= pat.Len {
		return st * float64(pat.Len)
	}
	t := st * float64(k)
	if pat.Swing[k] && pat.SwingAmt > 50 {
		t += (2*float64(pat.SwingAmt)/100 - 1) * st
	}
	return t
}

func (e *Engine) rewindLocked() {
	e.tick, e.nextStep, e.playStep = 0, 0, 0
	e.extTicks = 0
}

// trackActive reports whether a track's voice sounds now.
func (e *Engine) trackActive(t int) bool {
	if e.polyActive() {
		for _, v := range e.voices {
			if v.active {
				return true
			}
		}
		return false
	}
	return e.voices[t].active
}

// stepEventLocked fires whatever is due at the next step, or moves to the
// next pattern at the end of this one.
func (e *Engine) stepEventLocked() {
	pat := e.proj.Pattern(e.curPat)
	if e.nextStep >= pat.Len {
		e.tick -= e.fireTime(pat, pat.Len)
		if e.tick < 0 {
			e.tick = 0
		}
		e.advancePatternLocked()
		e.nextStep = 0
		return
	}
	k := e.nextStep
	e.playStep = k
	e.nextStep++
	tps := e.ticksPerSample()
	stTicks := float64(StepTicks(pat.Scale))
	stepSamples := int(stTicks / tps)
	for t := 0; t < NumTracks; t++ {
		if pat.Poly && t != pat.PolyTrack {
			continue
		}
		tp := &pat.Tracks[t]
		st := &tp.Step[k]
		if !st.On {
			continue
		}
		tr := &e.tr[t]
		tr.lockMask = 0
		if l := tp.Lock[k]; l != nil {
			tr.lockMask = l.Mask
			for i := 0; i < NParams; i++ {
				if l.Has(i) {
					tr.lockVal[i] = float32(l.Val[i]) / 127
				}
			}
		}
		if st.Off {
			e.releaseTrackLocked(t)
			continue
		}
		mask := st.Trig
		if pat.Slide[k] && e.trackActive(t) {
			// slide: the sound keeps going and the parameters glide to the locks
			tr.slideRemain = stepSamples
			mask = 0
		} else {
			tr.slideRemain = 0
		}
		e.fireStepLocked(t, pat, st, mask, stTicks, tps)
	}
}

// fireStepLocked plays the notes of one step.
func (e *Engine) fireStepLocked(t int, pat *Pattern, st *Step, mask uint8, stTicks, tps float64) {
	ts := &e.kit.Track[t]
	trans := ts.Trans
	if !ts.TransFix {
		trans += pat.Trans + e.songTrans
	}
	var raw [MaxChord]uint8
	n := st.Notes(&raw)
	var notes [MaxChord]uint8
	for i := 0; i < n; i++ {
		notes[i] = uint8(clampi(int(raw[i])+trans, 0, 127))
	}
	vel := float32(st.Vel) / 127
	lenSamples := int(float64(st.Len) * stTicks / LenUnits / tps)
	if lenSamples < 1 {
		lenSamples = 1
	}
	if Machines[ts.Machine].FX {
		e.voices[t].noteOn(float32(notes[0]), vel, mask&TrigLFO, 0, -1, e.age)
		return
	}
	if ts.Arp.Mode != ArpOff {
		e.arpSetSeq(t, notes[:n], vel, lenSamples)
		return
	}
	if pat.Poly {
		for i := 0; i < n; i++ {
			v := e.allocVoice(t)
			e.age++
			v.noteOn(float32(notes[i]), vel, mask, lenSamples, -1, e.age)
		}
		return
	}
	e.age++
	e.voices[t].noteOn(float32(notes[0]), vel, mask, lenSamples, -1, e.age)
}

// allocVoice picks the voice a new note of track t plays on: the track's own
// voice, or in poly mode a free one (else the oldest).
func (e *Engine) allocVoice(t int) *voice {
	if !e.polyActive() {
		return e.voices[t]
	}
	var best *voice
	for _, v := range e.voices {
		if !v.active {
			if best == nil || best.active || v.age < best.age {
				best = v
			}
		}
	}
	if best != nil {
		return best
	}
	best = e.voices[0]
	for _, v := range e.voices {
		if v.age < best.age {
			best = v
		}
	}
	return best
}

// releaseTrackLocked sends a note off to every voice that plays track t.
func (e *Engine) releaseTrackLocked(t int) {
	a := &e.tr[t].arp
	a.nSeq, a.seqLeft = 0, 0
	for _, v := range e.voices {
		if v.owner == t {
			v.noteOff()
		}
	}
}

// advancePatternLocked picks the pattern that follows the one that just
// ended: the next song row, a queued pattern, or the same one again.
func (e *Engine) advancePatternLocked() {
	next := e.curPat
	if e.songMode {
		s := e.proj.Song(e.song)
		if len(s.Rows) == 0 {
			e.playing = false
			return
		}
		e.songRep++
		if e.songRow < 0 || e.songRow >= len(s.Rows) {
			e.songRow, e.songRep = 0, 0
		} else if e.songRep >= s.Rows[e.songRow].Repeats {
			e.songRow++
			e.songRep = 0
			if e.songRow >= len(s.Rows) {
				if s.LoopTo >= 0 && s.LoopTo < len(s.Rows) {
					e.songRow = s.LoopTo
				} else {
					e.songRow = -1
					e.playing = false
					return
				}
			}
		}
		next = s.Rows[e.songRow].Pattern
		e.songTrans = s.Rows[e.songRow].Trans
	} else if e.queued >= 0 {
		next = e.queued
		e.queued = -1
	}
	if next != e.curPat {
		e.setPatternLocked(next)
	}
}

func (e *Engine) setPatternLocked(n int) {
	old := e.proj.Pattern(e.curPat)
	e.curPat = clampi(n, 0, NumPatterns-1)
	p := e.proj.Pattern(e.curPat)
	if k := p.Kit; k >= 0 && k != e.kitN {
		e.loadKitLocked(k)
	}
	if old.Poly != p.Poly || old.PolyTrack != p.PolyTrack || p.Poly {
		for _, v := range e.voices {
			v.noteOff()
		}
		e.syncVoicesLocked()
	}
}

// ------------------------------------------------------------ transport

// Play starts the sequencer from the top of the pattern (or of the song).
func (e *Engine) Play() {
	e.mu.Lock()
	defer e.mu.Unlock()
	if e.sync == SyncMIDI {
		return
	}
	e.startLocked()
}

func (e *Engine) startLocked() {
	if e.songMode {
		s := e.proj.Song(e.song)
		if len(s.Rows) == 0 {
			return
		}
		e.songRow, e.songRep = 0, 0
		e.songTrans = s.Rows[0].Trans
		e.setPatternLocked(s.Rows[0].Pattern)
	} else {
		e.songTrans = 0
	}
	e.rewindLocked()
	e.aclk = 0
	for t := range e.tr {
		e.tr[t].arp.on = false
	}
	e.extTarget = 0
	e.playing = true
}

// Stop halts the sequencer. Notes already sounding are released.
func (e *Engine) Stop() {
	e.mu.Lock()
	if e.sync == SyncMIDI {
		e.mu.Unlock()
		return
	}
	e.stopLocked()
	e.mu.Unlock()
}

func (e *Engine) stopLocked() {
	if !e.playing {
		return
	}
	e.playing = false
	for t := range e.tr {
		e.tr[t].arp.nSeq, e.tr[t].arp.seqLeft = 0, 0
	}
	for _, v := range e.voices {
		if v.noteID < 0 { // keys still held keep sounding
			v.noteOff()
		}
	}
}

// TogglePlay is Play when stopped and Stop when playing.
func (e *Engine) TogglePlay() {
	e.mu.Lock()
	defer e.mu.Unlock()
	if e.sync == SyncMIDI {
		return
	}
	if e.playing {
		e.stopLocked()
	} else {
		e.startLocked()
	}
}

// LocalTransport reports whether Play and Stop work from the module. With MIDI
// sync Live is the only transport: the module starts, stops and finds its place
// with Live (Follower, MIDI Start/Stop). A Play or Stop pressed here would only
// put it out of step with Live and with other modules, so Play, Stop and
// TogglePlay do nothing then.
func (e *Engine) LocalTransport() bool { e.mu.Lock(); defer e.mu.Unlock(); return e.sync != SyncMIDI }

// Playing reports whether the sequencer runs.
func (e *Engine) Playing() bool { e.mu.Lock(); defer e.mu.Unlock(); return e.playing }

// SetTempo sets the internal tempo, 30..300 BPM.
func (e *Engine) SetTempo(bpm float64) {
	e.mu.Lock()
	e.bpm = math.Min(300, math.Max(30, bpm))
	e.proj.Tempo = e.bpm
	e.dirty = true
	e.mu.Unlock()
}

// InternalTempo is the tempo set on the module.
func (e *Engine) InternalTempo() float64 { e.mu.Lock(); defer e.mu.Unlock(); return e.bpm }

// Tempo is the tempo in use: the internal one, or the one measured from the
// incoming MIDI clock.
func (e *Engine) Tempo() float64 {
	e.mu.Lock()
	defer e.mu.Unlock()
	return e.currentBPM()
}

// SetRecord turns real-time recording of played notes on or off.
func (e *Engine) SetRecord(on bool) { e.mu.Lock(); e.rec = on; e.mu.Unlock() }

// Recording reports record mode.
func (e *Engine) Recording() bool { e.mu.Lock(); defer e.mu.Unlock(); return e.rec }

// SelectPattern picks a pattern. While playing it is queued and starts when
// the current pattern ends, unless now is true. When stopped it is selected
// at once.
func (e *Engine) SelectPattern(n int, now bool) {
	e.mu.Lock()
	defer e.mu.Unlock()
	n = clampi(n, 0, NumPatterns-1)
	if e.songMode {
		return
	}
	if !e.playing || now {
		e.queued = -1
		e.setPatternLocked(n)
		if e.playing {
			e.rewindLocked()
		}
		return
	}
	if n == e.curPat {
		e.queued = -1
	} else {
		e.queued = n
	}
}

// SetSong turns song mode on (playing song n) or off.
func (e *Engine) SetSong(on bool, n int) {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.songMode = on
	e.song = clampi(n, 0, NumSongs-1)
	e.queued = -1
	if !on {
		e.songRow, e.songTrans = -1, 0
	}
}

// SongMode reports song mode and the song in use.
func (e *Engine) SongMode() (bool, int) { e.mu.Lock(); defer e.mu.Unlock(); return e.songMode, e.song }

// Position is the sequencer position, for the screens.
func (e *Engine) Position() Position {
	e.mu.Lock()
	defer e.mu.Unlock()
	row := -1
	if e.songMode {
		row = e.songRow
	}
	return Position{Playing: e.playing, Step: e.playStep, Pattern: e.curPat, Next: e.queued,
		Row: row, Repeat: e.songRep, Tick: e.tick}
}

// ---------------------------------------------------------- MIDI clock

// SetSync chooses the clock source.
func (e *Engine) SetSync(m SyncMode) { e.mu.Lock(); e.sync = m; e.mu.Unlock() }

// Sync is the clock source.
func (e *Engine) Sync() SyncMode { e.mu.Lock(); defer e.mu.Unlock(); return e.sync }

// ExtStart is MIDI Start: play from the top. Ignored unless sync is MIDI.
func (e *Engine) ExtStart() {
	e.mu.Lock()
	defer e.mu.Unlock()
	if e.sync != SyncMIDI {
		return
	}
	e.startLocked()
	e.extTarget = 0
}

// ExtJoin starts the sequencer in the middle of a song: Live was already
// playing when MonoModule came up, so no MIDI Start arrived. beat is Live's
// song position in quarter notes. The pattern starts where it would be had it
// been started with Live at beat 0, so the grids line up. The MIDI clock
// pulses that follow keep it there. Ignored unless sync is MIDI, or when the
// sequencer is already playing.
func (e *Engine) ExtJoin(beat float64) {
	e.mu.Lock()
	defer e.mu.Unlock()
	if e.sync != SyncMIDI || e.playing || beat < 0 || math.IsNaN(beat) || math.IsInf(beat, 0) {
		return
	}
	e.startLocked()
	if !e.playing { // an empty song: nothing to play
		return
	}
	pat := e.proj.Pattern(e.curPat)
	total := e.fireTime(pat, pat.Len)
	pos := math.Mod(beat*TicksPerBeat, total)
	k := 0
	for k < pat.Len && e.fireTime(pat, k) < pos-1e-6 {
		k++
	}
	e.tick, e.nextStep = pos, k
	e.playStep = clampi(k-1, 0, pat.Len-1)
	e.extTicks, e.extTarget = pos, pos
}

// ExtContinue is MIDI Continue.
func (e *Engine) ExtContinue() {
	e.mu.Lock()
	if e.sync == SyncMIDI {
		e.playing = true
	}
	e.mu.Unlock()
}

// ExtStop is MIDI Stop.
func (e *Engine) ExtStop() {
	e.mu.Lock()
	if e.sync == SyncMIDI {
		e.stopLocked()
	}
	e.mu.Unlock()
}

// ExtPulse is one MIDI clock pulse (24 per quarter note). now is when it
// arrived. It measures the tempo and tells the sequencer where the clock is.
func (e *Engine) ExtPulse(now time.Time) {
	e.mu.Lock()
	defer e.mu.Unlock()
	if e.sync != SyncMIDI {
		return
	}
	if !e.extLast.IsZero() {
		dt := now.Sub(e.extLast).Seconds()
		if dt > 0.0005 && dt < 0.5 {
			inst := 60 / (dt * 24)
			if e.extBPM == 0 || math.Abs(inst-e.extBPM) > e.extBPM*0.5 {
				e.extBPM = inst
			} else {
				e.extBPM += (inst - e.extBPM) * 0.05
			}
			e.extBPM = math.Min(300, math.Max(30, e.extBPM))
		} else if dt >= 0.5 {
			e.extBPM = 0
		}
	}
	e.extLast = now
	e.extTarget += TicksPerBeat / 24
}

// ------------------------------------------------------------ playing

// NoteOn plays a note on a track now, as a pad or a MIDI key does. While
// the sequencer plays in record mode the note is written to the pattern.
func (e *Engine) NoteOn(track, note int, vel float32) {
	e.mu.Lock()
	defer e.mu.Unlock()
	if !okTrack(track) {
		return
	}
	note = clampi(note, 0, 127)
	if vel <= 0 {
		vel = 0.8
	}
	pat := e.proj.Pattern(e.curPat)
	if pat.Poly && pat.PolyTrack != track {
		return
	}
	tr := &e.tr[track]
	tr.lockMask, tr.slideRemain = 0, 0
	ts := &e.kit.Track[track]
	if Machines[ts.Machine].FX {
		e.voices[track].noteOn(float32(note), vel, TrigLFO, 0, -1, e.age)
	} else if ts.Arp.Mode != ArpOff && e.arpAddLive(track, note, vel) {
		// the arpeggiator plays it
	} else {
		v := e.reuseVoice(track, note)
		e.age++
		v.noteOn(float32(note), vel, TrigAll, 0, note, e.age)
	}
	if e.rec {
		e.recordNoteLocked(track, note, vel)
	}
}

// reuseVoice finds the voice already playing note (a repeated key), or a
// new one.
func (e *Engine) reuseVoice(track, note int) *voice {
	if e.polyActive() {
		for _, v := range e.voices {
			if v.noteID == note && v.active {
				return v
			}
		}
	}
	return e.allocVoice(track)
}

// NoteOff releases a note played with NoteOn.
func (e *Engine) NoteOff(track, note int) {
	e.mu.Lock()
	defer e.mu.Unlock()
	if !okTrack(track) {
		return
	}
	note = clampi(note, 0, 127)
	if e.kit.Track[track].Arp.Mode != ArpOff {
		e.arpRemoveLive(track, note)
	}
	for _, v := range e.voices {
		if v.noteID == note && v.owner == track {
			v.noteOff()
		}
	}
	if ln := &e.live[note]; ln.on && ln.track == track {
		ln.on = false
		if e.rec {
			e.finishRecordedLocked(ln, note)
		}
	}
}

// AllNotesOff releases everything that is held or sounding.
func (e *Engine) AllNotesOff() {
	e.mu.Lock()
	defer e.mu.Unlock()
	for _, v := range e.voices {
		v.noteOff()
	}
	for t := range e.tr {
		a := &e.tr[t].arp
		a.nLive, a.nSeq, a.held, a.seqLeft = 0, 0, 0, 0
	}
	for i := range e.live {
		e.live[i].on = false
	}
}

// recordNoteLocked writes a played note into the pattern at the nearest step.
func (e *Engine) recordNoteLocked(track, note int, vel float32) {
	pat := e.proj.Pattern(e.curPat)
	k := e.stepCursor
	if e.playing {
		st := float64(StepTicks(pat.Scale))
		k = int(math.Floor(e.tick/st + 0.5))
		if k >= pat.Len {
			k = 0
		}
	} else {
		e.stepCursor = (e.stepCursor + 1) % pat.Len // step recording advances
	}
	tp := &pat.Tracks[track]
	cur := &tp.Step[k]
	ns := NewNoteStep(note, int(vel*127+0.5))
	if cur.On && !cur.Off && pat.Poly && e.live[note].on == false && e.chordWindow(track, k) {
		cur.AddNote(note)
	} else {
		*cur = ns
		tp.Lock[k] = nil
	}
	e.live[note] = liveNote{on: true, track: track, pat: e.curPat, step: k, startTk: e.tick}
	if !e.playing {
		e.live[note].on = false // no length to measure
	}
	e.dirty = true
}

// chordWindow reports whether another note of the same step was just played,
// so poly recording builds a chord instead of replacing the step.
func (e *Engine) chordWindow(track, k int) bool {
	for n := range e.live {
		if l := &e.live[n]; l.on && l.track == track && l.step == k && l.pat == e.curPat {
			return true
		}
	}
	return false
}

func (e *Engine) finishRecordedLocked(l *liveNote, note int) {
	pat := e.proj.Pattern(l.pat)
	st := float64(StepTicks(pat.Scale))
	held := e.tick - l.startTk
	if held < 0 {
		held += st * float64(pat.Len)
	}
	units := int(math.Round(held * LenUnits / st))
	s := &pat.Tracks[l.track].Step[l.step]
	if s.On && int(s.Note) == note {
		s.Len = uint8(clampi(units, 1, 127))
		e.dirty = true
	}
}

// StepCursor is the position of step recording (record on, sequencer stopped).
func (e *Engine) StepCursor() int { e.mu.Lock(); defer e.mu.Unlock(); return e.stepCursor }

// SetStepCursor moves the step recording position.
func (e *Engine) SetStepCursor(k int) {
	e.mu.Lock()
	e.stepCursor = clampi(k, 0, MaxSteps-1)
	e.mu.Unlock()
}
