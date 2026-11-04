package engine

import "testing"

// With MIDI sync Live is the only transport: Play, Stop and TogglePlay from the
// module change nothing. With internal sync they work.
func TestMIDISyncMakesLiveTheOnlyTransport(t *testing.T) {
	e := newTest()
	e.SetSync(SyncMIDI)
	if e.LocalTransport() {
		t.Fatal("LocalTransport is true with MIDI sync")
	}
	e.Play()
	e.TogglePlay()
	if e.Playing() {
		t.Fatal("Play or TogglePlay started the sequencer with MIDI sync")
	}
	e.ExtStart() // Live starts it
	if !e.Playing() {
		t.Fatal("Live's Start did not start the sequencer")
	}
	e.Stop()
	e.TogglePlay()
	if !e.Playing() {
		t.Fatal("Stop or TogglePlay stopped the sequencer with MIDI sync")
	}
	e.ExtStop() // Live stops it
	if e.Playing() {
		t.Fatal("Live's Stop did not stop the sequencer")
	}
	e.SetSync(SyncInternal)
	e.TogglePlay()
	if !e.Playing() || !e.LocalTransport() {
		t.Fatal("internal sync: Play must work")
	}
}
