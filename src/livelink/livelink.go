// Package livelink talks to the PushHack Remote Script in Ableton Live
// (127.0.0.1:7704, JSON lines) to make MonoModule's two tracks. Plain Go.
package livelink

import (
	"bufio"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"regexp"
	"strconv"
	"strings"
	"time"
)

const (
	Addr       = "127.0.0.1:7704"
	AudioTrack = "MonoModule"
	MIDITrack  = "MonoModule MIDI"
	MIDIPort   = "MonoModule MIDI In"
	timeout    = 5 * time.Second
)

// Error is a request the script refused.
type Error struct {
	Code string
	Data json.RawMessage
}

func (e *Error) Error() string { return e.Code }

// Caller sends one request and returns the reply data. Tests replace it.
type Caller func(ns, cmd string, args map[string]any) (json.RawMessage, error)

// Call is the real Caller: one connection per request.
func Call(ns, cmd string, args map[string]any) (json.RawMessage, error) {
	req := map[string]any{"id": 1, "ns": ns, "cmd": cmd}
	for k, v := range args {
		req[k] = v
	}
	b, err := json.Marshal(req)
	if err != nil {
		return nil, err
	}
	conn, err := net.DialTimeout("tcp", Addr, 2*time.Second)
	if err != nil {
		return nil, err
	}
	defer conn.Close()
	_ = conn.SetDeadline(time.Now().Add(timeout))
	if _, err := conn.Write(append(b, '\n')); err != nil {
		return nil, err
	}
	line, err := bufio.NewReader(conn).ReadBytes('\n')
	if len(line) == 0 && err != nil {
		return nil, err
	}
	var r struct {
		OK    *bool           `json:"ok"`
		Error string          `json:"error"`
		Data  json.RawMessage `json:"data"`
	}
	if json.Unmarshal(line, &r) != nil || r.OK == nil {
		return nil, errors.New("the Remote Script does not answer like PushHack: install or update Push Hack")
	}
	if !*r.OK {
		return nil, &Error{Code: r.Error, Data: r.Data}
	}
	return r.Data, nil
}

var pairRe = regexp.MustCompile(`^(\d+)/(\d+)$`)

// PairName is Live's name for the stereo pair starting at 0-based offset.
func PairName(offset int) string { return fmt.Sprintf("%d/%d", offset+1, offset+2) }

// ParsePair is the inverse; ok is false for a mono name or a split pair.
func ParsePair(name string) (int, bool) {
	m := pairRe.FindStringSubmatch(name)
	if m == nil {
		return 0, false
	}
	a, _ := strconv.Atoi(m[1])
	b, _ := strconv.Atoi(m[2])
	if a < 1 || b != a+1 {
		return 0, false
	}
	return a - 1, true
}

// ChannelName is Live's name for the input at 0-based offset: "3" for mono,
// "3/4" for a stereo pair.
func ChannelName(offset int, mono bool) string {
	if mono {
		return strconv.Itoa(offset + 1)
	}
	return PairName(offset)
}

var monoRe = regexp.MustCompile(`^(\d+)$`)

// ParseMono is the inverse for a single channel; "3/4" and "0" are not ok.
func ParseMono(name string) (int, bool) {
	if !monoRe.MatchString(name) {
		return 0, false
	}
	n, _ := strconv.Atoi(name)
	if n < 1 {
		return 0, false
	}
	return n - 1, true
}

func routing(e *Error) (what string, options []string) {
	var d struct {
		What    string   `json:"what"`
		Options []string `json:"options"`
	}
	_ = json.Unmarshal(e.Data, &d)
	return d.What, d.Options
}

// Result is what the web page shows after "Create tracks".
type Result struct {
	Lines []string `json:"lines"`
}

// EnsureTracks makes an audio track that reads the module's input (a pair or
// a single channel) and a MIDI track that plays into MonoModule. It never
// changes the module's output: if Live does not offer the input, the error
// names what Live offers, and the user picks one of those on the web page.
func EnsureTracks(call Caller, offset int, mono bool) (Result, error) {
	var res Result
	audio := map[string]any{
		"kind": "audio", "name": AudioTrack,
		"input_type": "Ext. In", "input_channel": []string{ChannelName(offset, mono)},
		"output_type": "Main", "monitoring": 0,
	}
	data, err := call("track", "ensure", audio)
	var pe *Error
	if errors.As(err, &pe) && pe.Code == "bad_routing" {
		if what, opts := routing(pe); what == "channel" {
			return res, fmt.Errorf("Live does not offer input %s on Ext. In (it offers: %s). Enable that input of the Push Hack Virtual Audio card in Push's audio preferences, or pick a channel MonoModule can use", ChannelName(offset, mono), strings.Join(opts, ", "))
		}
	}
	if err != nil {
		return res, Friendly("audio track", err)
	}
	res.Lines = append(res.Lines, line(AudioTrack, created(data)))
	return ensureMIDI(call, res)
}

// EnsureMIDITrack makes only the MIDI track. Link mode uses it: the audio
// track there takes the PushHack input, which the user sets in Live.
func EnsureMIDITrack(call Caller) (Result, error) { return ensureMIDI(call, Result{}) }

func ensureMIDI(call Caller, res Result) (Result, error) {
	data, err := call("track", "ensure", map[string]any{
		"kind": "midi", "name": MIDITrack,
		"input_type": "All Ins", "output_type": MIDIPort,
	})
	if err != nil {
		return res, Friendly("MIDI track", err)
	}
	if r := parse(data); r.Created {
		_, _ = call("track", "select", map[string]any{"track": r.Track})
		_, _ = call("track", "set", map[string]any{"track": r.Track, "arm": true})
		res.Lines = append(res.Lines, line(MIDITrack, true), "Selected and armed. Notes on MIDI channels 1-6 play tracks 1-6.")
	} else {
		res.Lines = append(res.Lines, line(MIDITrack, false))
	}
	return res, nil
}

type ensureReply struct {
	Track   any  `json:"track"`
	Created bool `json:"created"`
}

func parse(d json.RawMessage) ensureReply {
	var r ensureReply
	_ = json.Unmarshal(d, &r)
	return r
}
func created(d json.RawMessage) bool { return parse(d).Created }

func line(name string, created bool) string {
	if created {
		return fmt.Sprintf("Created the %q track.", name)
	}
	return fmt.Sprintf("The %q track was already there; routing set.", name)
}

// Friendly turns a failed request into a sentence.
func Friendly(what string, err error) error {
	var pe *Error
	if errors.As(err, &pe) {
		switch pe.Code {
		case "bad_routing":
			_, opts := routing(pe)
			return fmt.Errorf("%s: Live does not offer that routing (it offers: %s)", what, strings.Join(opts, ", "))
		case "wrong_track_kind":
			return fmt.Errorf("%s: a track with that name exists but is the wrong kind; rename it in Live", what)
		case "unknown_ns", "unknown_cmd":
			return fmt.Errorf("%s: the PushHack Remote Script is too old (needs 0.5.0 or later); update Push Hack and restart Live", what)
		}
		return fmt.Errorf("%s: %s", what, pe.Code)
	}
	return fmt.Errorf("%s: %w (is the PushHack Remote Script selected in Live?)", what, err)
}

// Transport is Live's transport, from the Remote Script's transport.get.
type Transport struct {
	Playing bool    `json:"playing"`
	Tempo   float64 `json:"tempo"`
	Beat    float64 `json:"beat"` // song position in quarter notes
}

// ReadTransport asks Live whether it is playing and where.
func ReadTransport(call Caller) (Transport, error) {
	var t Transport
	data, err := call("transport", "get", nil)
	if err != nil {
		return t, Friendly("transport", err)
	}
	if err := json.Unmarshal(data, &t); err != nil {
		return t, fmt.Errorf("transport: %w", err)
	}
	return t, nil
}
