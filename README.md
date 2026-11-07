# MonoModule

A six-voice synthesizer for **Ableton Push 3** in the style of the Elektron Monomachine SFX-60 MKII, as a module for [Push Hack](https://github.com/federico-pepe/ableton-push-hack).

It follows the Monomachine's *model*: six tracks, 22 machines, a cascaded high-pass/low-pass filter with an envelope, three LFOs per voice, per-track EQ, sample-rate reduction and delay, a pitched step sequencer with parameter locks, slide, swing, a POLY mode, an arpeggiator, kits, patterns and songs. The sound engine is original; it is **not** a reproduction of Elektron's DSP and does not sound identical to the hardware.

**Not affiliated with, endorsed by or derived from Elektron.** "Monomachine" is Elektron's name; it is used here only to say what this imitates.

## Status: alpha, not verified on hardware

Everything that can run without a Push has been tested here (engine, sequencer, panel, project files, MIDI map, web API, the cgo audio path against a stand-in for libasound). **The integration with a real Push 3, Ableton Live and the loopback card has not been run**: Home focus/blur, LED updates, pad and encoder mapping, the audio session on `hw:PHVAudio,1,0` and track creation through the Remote Script are written from the patterns of other Push Hack modules and compile-checked only. The audio mixer client (below) is tested off device against a real `core/audiobus` server; the same client code ran on a Push 3 in MachineModule and Braids, but MonoModule itself has not. See [CLAUDE.md](CLAUDE.md) for what to check first on a device.

## What you get

- **Six tracks, 22 machines**: GND-EMPTY/SIN/NOIS, SWAVE-SAW/PULSE/ENS, SID-6581, VO-6, FM+STAT/PAR/DYN, DPRO-WAVE/BBOX/DDRW/DENS, and the FX machines THRU, REVERB, CHORUS, DYNAMIX, RINGMOD, PHASER, FLANGER (which process lower-numbered tracks).
- **56 parameters per track** on seven pages of eight (SYN, AMP, FLT, FX, LFO1-3), all lockable per step.
- **Sequencer**: patterns A01-H16, 2-64 steps, scales 1/2x-1x-2x, swing 50-80%, slide, chords, note-off steps, trigless trigs, real-time and step recording.
- **POLY**: all six voices on one track.
- **Arpeggiator** per track (up, down, up/down, down/up, random; NORM, LATCH, SID play modes).
- **128 kits, 24 songs**, 32 user waveforms (WAV import).
- **Push front panel** on a green LCD look-alike, and a **web page** with the same functions plus a piano keyboard.
- MIDI in: channel n plays track n, CCs 16-71 map a track's 56 parameters, clock/start/stop sync (with MIDI sync Live is the only transport: Play and Stop in the module do nothing); with sync on MIDI it also joins when Live is already playing, and stops when Live stops (read from the PushHack Remote Script).

## Requirements

[push-audio-loopback](https://github.com/federico-pepe/push-hack-audio-loopback) (it sends the audio to Live over Ableton Link Audio, or through a virtual audio card), push-manager and push-display, and the PushHack Remote Script in Live (to create the tracks). For Link Audio: push-audio-loopback 0.3.0-alpha or later. For the audio mixer below: 0.2.0-alpha or later.

## Link Audio (default)

push-audio-loopback 0.3.0-alpha sends the audio of MonoModule to Live over Ableton Link Audio. This is its default.
MonoModule is then its own Link channel, named `MonoModule`, and Live keeps the sound card of the Push as its output.

1. In Live, turn on **Link** and **Link Audio**. Live switches them off after a restart.
2. Give an audio track the input type **PushHack**, pick the channel `MonoModule`, and set Monitor to In.
3. The Audio page of push-audio-loopback (Push Hack menu, *Audio*) switches between Link and the virtual card.

- The channel pair on the web page does not matter in Link mode.
- The *Create tracks in Live* button still makes the audio track on the virtual card. In Link mode, set the input of that track by hand as in step 2.
- After a restart of push-audio-loopback, MonoModule can drift against the clock of Live. Press play in Live and it is back in time. This is a known issue.

## Audio mixer

This section is about the virtual card mode.

MonoModule plays through the audio mixer of **push-audio-loopback** when it runs. The mixer adds the audio of all audio hacks, so MonoModule and Braids can play at the same time. The mixer owns the card and plays stereo pairs: the web page's *System* tab then offers the channel pair (the mixer remembers it), and the *Output* (stereo/mono) and device lists are off. Without the mixer, MonoModule writes to the card itself, with stereo or mono output and any device, and only one audio hack can do this at a time. It switches by itself when the mixer starts or stops.

*Create the audio and MIDI tracks in Live* never changes your channel pair. If Live does not offer the input, it says which inputs Live offers; enable the input in Push's audio preferences, or pick one of those channels on the web page.

The design is in [docs/audio-bus.md](https://github.com/federico-pepe/ableton-push-hack/blob/main/docs/audio-bus.md) in the main Push Hack repository.

## Install and build

Add `catalog-entry.json` to the framework's catalog, tag a release (see [CLAUDE.md](CLAUDE.md)), or build locally:

```
make          # builds ./monomodule for the Push (Docker, cgo, linux/amd64)
make test     # go vet + go test -race
make smoke    # off-device test of the audio path and web API
scripts/deploy.sh <push-host>
```

`go.sum` is not committed; `make` runs `go mod tidy` in the container. `core/audiobus` and `alsaseq.Push3` come from `core/v0.5.4`, pinned in `src/go.mod`.

## Use

Open it from Push Hack Home (Shift + Help). Then see [MANUAL.md](MANUAL.md). In short: pads rows 0-1 are steps, row 2 selects tracks, rows 3-4 select patterns, rows 5-7 play notes; the eight encoders edit the page's eight parameters; hold a step and turn an encoder to lock that parameter on the step.

## What is not implemented

The Monomachine has features this module leaves out: the MIDI sequencer tracks, multi-trig/multi-envelope modes, audio inputs, the three stereo output buses, FX machines gated by trigs, SysEx dump/receive, the +Drive/RAM memory and the joystick. Machine names and parameter names are this project's own (the manual's appendix could not be read), and several behaviours are approximations. Details are in the MANUAL's "Differences" section.

## Docs

`docs/screens` has the Push screens (rendered by `cmd/mmscreens` from the same code the device runs) and the web page; `docs/demo.wav` is a 32 s demo rendered by `cmd/mmrender` without any hardware.

## License

MIT, see LICENSE.
