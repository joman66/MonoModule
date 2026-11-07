# Sharing the audio output

Several audio hacks (MachineModule, MonoModule, Braids) play at the same time through push-audio-loopback.

## Link Audio (default)

- Each hack is its own Link channel, named after the hack: `MachineModule`, `MonoModule`, `Braids`. In Live, give an audio track the input type **PushHack** and pick the channel.
- Live keeps the sound card of the Push as its output. Turn on Link and Link Audio in Live. Live switches them off after a restart.
- No channel pair is needed. The pair on the web page applies to the virtual card mode only.

## Virtual card mode

- push-audio-loopback adds the audio of all hacks and writes the card. Give every module its own pair, for example MachineModule 1/2 and MonoModule 3/4. Two modules on the same pair sum in Live.
- Enable each input in Live (Push audio preferences > Devices > Push Hack Virtual Audio). Only enabled inputs are offered.

## Names

Module names are unique: audio track `MonoModule`, MIDI track `MonoModule MIDI`, MIDI port `MonoModule MIDI In`.

## Checked

MachineModule, MonoModule and Braids played together, in both modes, on a Push 3 (2026-11-07). The older text about dmix is not needed: the mixer owns the card, and the hacks do not open it.

## Known issue

After a restart of push-audio-loopback the hacks can drift against the clock of Live. Press play in Live.
