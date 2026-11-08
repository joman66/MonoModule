package livelink

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestPair(t *testing.T) {
	if PairName(2) != "3/4" {
		t.Fatal(PairName(2))
	}
	if o, ok := ParsePair("3/4"); !ok || o != 2 {
		t.Fatal("3/4")
	}
	for _, bad := range []string{"3", "3/5", "0/1", "a/b"} {
		if _, ok := ParsePair(bad); ok {
			t.Fatal(bad)
		}
	}
}

// Live offers only the pair 5/6: the module must not switch to it on its own.
func TestEnsureKeepsChosenChannel(t *testing.T) {
	asked := 0
	call := func(ns, cmd string, args map[string]any) (json.RawMessage, error) {
		if cmd == "ensure" && args["kind"] == "audio" {
			asked++
			return nil, &Error{Code: "bad_routing", Data: json.RawMessage(`{"what":"channel","options":["1","2","5/6"]}`)}
		}
		t.Fatalf("unexpected call %s.%s", ns, cmd)
		return nil, nil
	}
	_, err := EnsureTracks(call, 0, false)
	if err == nil || asked != 1 || !strings.Contains(err.Error(), "5/6") {
		t.Fatalf("%v (asked %d)", err, asked)
	}
}

func TestEnsureMakesAudioAndMIDITracks(t *testing.T) {
	var kinds []string
	call := func(ns, cmd string, args map[string]any) (json.RawMessage, error) {
		if cmd == "ensure" {
			kinds = append(kinds, args["kind"].(string))
		}
		return json.RawMessage(`{"track":1,"created":true}`), nil
	}
	res, err := EnsureTracks(call, 2, true)
	if err != nil || len(kinds) != 2 || kinds[0] != "audio" || kinds[1] != "midi" || len(res.Lines) < 3 {
		t.Fatalf("%v %v %v", err, kinds, res.Lines)
	}
}

func TestEnsureMIDITrackOnly(t *testing.T) {
	var kinds []string
	call := func(ns, cmd string, args map[string]any) (json.RawMessage, error) {
		if cmd == "ensure" {
			kinds = append(kinds, args["kind"].(string))
		}
		return json.RawMessage(`{"track":1,"created":true}`), nil
	}
	res, err := EnsureMIDITrack(call)
	if err != nil || len(kinds) != 1 || kinds[0] != "midi" || len(res.Lines) < 2 {
		t.Fatalf("%v %v %v", err, kinds, res.Lines)
	}
}

func TestEnsureReportsMissingPair(t *testing.T) {
	call := func(ns, cmd string, args map[string]any) (json.RawMessage, error) {
		return nil, &Error{Code: "bad_routing", Data: json.RawMessage(`{"what":"channel","options":["1/2","3/4"]}`)}
	}
	if _, err := EnsureTracks(call, 6, false); err == nil {
		t.Fatal("expected error")
	}
}

func TestChannelNames(t *testing.T) {
	if ChannelName(2, true) != "3" || ChannelName(2, false) != "3/4" {
		t.Fatal("names")
	}
	if o, ok := ParseMono("3"); !ok || o != 2 {
		t.Fatal("3")
	}
	for _, bad := range []string{"3/4", "0", "", "x"} {
		if _, ok := ParseMono(bad); ok {
			t.Fatal(bad)
		}
	}
}
