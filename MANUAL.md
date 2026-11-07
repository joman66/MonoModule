# MonoModule manual

## Concepts

- **Track / voice**: six tracks, each with its own voice (one note at a time) and its own machine. In **POLY** mode the six voices all play the poly track.
- **Machine**: the sound generator of a track. SYN page parameters are named by the machine.
- **Kit**: the six tracks' machines and 56 parameters each, plus the master strip. The *working kit* is what you hear; kits are stored in 128 slots.
- **Pattern**: six tracks of up to 64 steps, A01-H16 (128). A pattern can load a kit when it starts.
- **Step**: a trig with a note (chords in POLY), velocity, length, which envelopes it restarts, and optional parameter locks.
- **Song**: up to 200 rows of pattern, repeats and transpose, with an optional loop-to row. 24 songs.

## Machines

| # | Machine | Group | SYN parameters |
|---|---|---|---|
| 1 | GND-EMPTY | GND | (silent; for tracks that only process) |
| 2 | GND-SIN | GND | TUNE HARM FOLD SUB FDBK DRFT PHAS BITS |
| 3 | GND-NOIS | GND | HPF LPF RESO COLR CRCK SHLD LEVL TILT |
| 4 | SWAVE-SAW | SWAVE | UNIW UNIL EXTL SQ-1 SI-1 SI-2 TUNE DRFT |
| 5 | SWAVE-PULSE | SWAVE | PW PWMD PWMS DETU MIX SUB TUNE DRFT |
| 6 | SWAVE-ENS | SWAVE | DETU SPRD CHRD INV CHRL CHRW TUNE DRFT |
| 7 | SID-6581 | SID | WAVE PW PWMD MODE MOFS MDET TUNE DRFT |
| 8 | VO-6 | VO | VOW1 VOW2 MORF CONS CLEN CVOL SIBI BRTH |
| 9 | FM+STAT | FM+ | M1FR M1LV M2FR M2LV FDBK TONE TUNE DRFT |
| 10 | FM+PAR | FM+ | M1FR M2FR M3FR M1LV M2LV M3LV MENV TUNE |
| 11 | FM+DYN | FM+ | M1FR M1LV M2FR M2LV FDBK MENV MDEC XPON |
| 12 | DPRO-WAVE | DPRO | WAVE MORF SYNC PHAS PRAT TUNE BITS DRFT |
| 13 | DPRO-BBOX | DPRO | SAMP PTCH STRT RTRG RTIM BITS REV DECY |
| 14 | DPRO-DDRW | DPRO | WVA WVB MIX TIME PHAS SYNC DETU BITS |
| 15 | DPRO-DENS | DPRO | WAVE PCH2 PCH3 PCH4 CHRL CHRW TUNE DRFT |
| 16 | FX-THRU | FX | GAIN BAL SAT WID |
| 17 | FX-REVERB | FX | DEC DAMP GATE HPF LPF PRED WET SIZE |
| 18 | FX-CHORUS | FX | DPTH SPD DLAY WID FDBK MIX HPF LPF |
| 19 | FX-DYNAMIX | FX | THR RAT ATK REL MKUP MIX LIM SCHP |
| 20 | FX-RINGMOD | FX | FREQ FINE SHPE MIX FMDP FMSP HPF LPF |
| 21 | FX-PHASER | FX | DPTH SPD FDBK STGS CNTR SPRD MIX WID |
| 22 | FX-FLANGER | FX | DPTH SPD DLAY FDBK MIX WID POL HPF |

These names are this project's own; the sounds are original. DPRO-WAVE plays your WAV files as single cycles (first parameter picks the slot, 1-32); DPRO-BBOX has 24 synthesized percussion sounds; the other DPRO machines use the 32 built-in wave tables.

**FX machines** process the tracks you tick under *Track & arp > FX input from* (or KIT page, SRC encoders on Push). Sources must be lower-numbered tracks, and a source that feeds an FX track no longer goes to the main mix. An FX track makes no sound of its own and plays no notes.

## The parameter pages

| Page | Parameters |
|---|---|
| SYN | eight, named by the machine |
| AMP | ATK HOLD DEC REL DIST VOL PAN PORT. Envelope is attack-hold-decay-release. HOLD 127 holds until note off. |
| FLT | BASE WDTH HPQ LPQ ATK DEC BOFS WOFS. BASE is a 24 dB high-pass; the 24 dB low-pass sits at BASE+WDTH. The AD envelope adds BOFS to BASE and WOFS to WDTH (64 = none). |
| FX | EQF EQG SRR DSND DTIM DFB DBAS DWID: one-band EQ, sample-rate reduction, and a tempo-synced delay (below 64 stereo, above 64 ping-pong). |
| LFO1-3 | PAGE DEST TRIG WAVE MULT SPD INTL DPTH. The LFO modulates one parameter of the voice (or pitch) in the chosen page. TRIG: FREE, TRIG, HOLD, ONE, HALF. 11 waves. SPD is tempo-synced. DPTH 64 = none. |

## Push 3 front panel

**Pads** (row 0 is the bottom):

| Rows | Function |
|---|---|
| 0-1 | Steps. Row 1 is steps 1-8, row 0 steps 9-16 of the current page (Page left/right for more). |
| 2 | Tracks 1-6 (columns 1-6). |
| 3-4 | Patterns of the bank chosen with the scene buttons (top = A). Press to queue; Shift+press to switch now. |
| 5-7 | Chromatic keyboard, 24 keys, octave with Octave up/down. |

**Steps.** Tap an empty pad to add a trig (it takes the note of the previous trig). Tap a trig again to remove it. *Hold* a step: play a pad key to set its note (more keys add to the chord in POLY); turn encoders 1-4 on SEQ to edit note, velocity, length, trig type; turn a page encoder on SYN/AMP/FLT/FX/LFO to write a **parameter lock** (marked on the ribbon). `CLR LOCK` (soft key 8) removes the held step's locks. Swing and Repeat buttons switch the step pads to the swing and slide layers; a slide step glides locked parameters without retriggering.

**Top buttons**: SYN, AMP, FLT, FX, LFO (press again for LFO2, LFO3), SEQ (again: ARP), KIT (again: MASTER), SONG. The Settings button opens SYSTEM.

**Keys**: Play (Shift+Play restarts), Record (play pad keys into the pattern; stopped = step recording), Tap tempo, Mute then track pads, Solo+track pad, Delete+pad clears a step/track/pattern, Duplicate+pattern pad copies the current pattern, Convert pastes, Save, Shift+step pad sets the pattern length, D-pad left/right selects track, up/down selects pattern, Jog press = enter/assign, Undo = cancel.

**SEQ page**: LENGTH SCALE SWING PAT-KIT TRANS POLY POLY-TRACK TEMPO. **ARP sub page**: MODE SPEED RANGE LEN PLAY TRACK-TRANS TRANS-FIX TEMPO. **KIT page**: kit slot, key-tracking, FX sources; Load/Save/Init on soft keys. **MASTER**: volume, compressor, tilt EQ. **SONG**: row, pattern, repeats, loop, song number, transpose; Add/Delete row; Song on/off. **SYSTEM**: UW slot, file, sync (INT/MIDI), load/clear waveform, save.

The big encoder is master volume; the tempo encoder sets tempo (Shift = fine).

## Web page

Same functions with a mouse: transport, six machine selectors, parameter sliders, step grid with note names and layers, step editor and locks, arp and track options, kits, master, songs, WAV upload, MIDI/audio config, and the button that creates the audio and MIDI tracks in Live. The keyboard at the bottom plays the selected track (also Z-M and Q-U); tick "enter notes into selected step" to write notes.

## Audio output

By default push-audio-loopback sends MonoModule to Live over Ableton Link Audio. In Live, turn on Link and Link Audio, give an audio track the input type *PushHack*, pick the channel *MonoModule* and set Monitor to In. The channel pair below applies to the virtual card mode only. The *Create the audio and MIDI tracks in Live* button makes its audio track on the virtual card, so in Link mode set the input by hand. After a restart of push-audio-loopback, press play in Live to bring the sequencer back in time.

Virtual card mode: MonoModule sends its audio to the audio mixer of push-audio-loopback when it runs, so it can play together with other audio hacks such as Braids. In the web page's *System* tab, choose the channel pair; the mixer remembers it. In Live, enable that pair on the input of the Push Hack Virtual Audio card and give an audio track that input (the *Create the audio and MIDI tracks in Live* button does this; it never changes your pair). The mixer plays stereo pairs, so the *Output* mode and the device list are off while it runs.

Without the mixer, MonoModule writes to the card itself: choose the device, the stereo or mono output and the channel (or pair). Only one audio hack can do this at a time. MonoModule switches between the two modes by itself.

## MIDI

External notes: MIDI channel *n* (from the base channel, default 1) plays track *n* at pitch; pitch bend on the base channel bends all tracks +-2 semitones. CCs 16-71 on a track's channel set parameters 1-56 of that track. On the base channel: CC 112 master volume, 113 tempo (30-157), 114 mute all (127) / unmute (0). CC 123 stops all notes. Program change selects a pattern. MIDI clock, start, continue and stop are followed when the sync is MIDI (Settings > SYSTEM, or the web page). The sync is MIDI by default and is not saved; set it to INT for the sequencer's own tempo.

MIDI Start only reaches MonoModule at the moment Live starts. So that it also follows when Live is already playing (you opened MonoModule, or switched the sync to MIDI, in the middle of a song), MonoModule asks the PushHack Remote Script in Live whether it plays and where. When Live changes from stopped to playing, the sequencer joins at Live's position (the pattern lines up with Live's beats as if it had started with Live at beat 0). When Live stops, it stops. The tempo follows Live too: from the MIDI clock while it arrives, and otherwise from the Remote Script (Live sends no clock unless you turn on Sync for MonoModule's MIDI port, and none while it is stopped), so a tempo change in Live reaches the sequencer and the tempo-synced delay either way. It only reacts to a change of Live's transport, so a sequencer you started by hand stays on until Live stops after playing. Without the Remote Script, only MIDI Start, Stop and the clock work.

## Differences from the Monomachine

Added: a master strip (volume, compressor, tilt EQ, soft limiter), a 1/2x scale, user waveforms from WAV files. Left out: MIDI sequencer tracks, multi-trig/multi-envelope, audio inputs, three stereo output buses, trig-gated FX machines, SysEx, +Drive/RAM, joystick, global effects (the hardware's per-track delay is kept; the global reverb of other Elektron boxes is not). Names and values of machines are this project's own; there is no attempt to match Elektron's sound.
