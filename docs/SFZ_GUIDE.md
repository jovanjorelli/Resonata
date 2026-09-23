# SFZ Guide

Resonata loads orchestral sample libraries via the SFZ format: a
fault-tolerant parser, WAV decoding, sample-path sanitizing, `#include`
expansion with cycle detection, and case-insensitive fallback on Linux
and macOS. Regions inherit defaults down `<global>` → `<group>` →
`<region>`. Values outside documented ranges are dropped; regions
without a `sample` are discarded; unknown opcodes are silently ignored.

## Supported opcodes

### Zone mapping

| Opcode | Range | Description |
|--------|-------|-------------|
| `sample` | path | WAV file relative to the SFZ location, required per region |
| `lokey` / `hikey` | 0-127 or pitch name | Key range triggering the region |
| `key` | 0-127 | Sets `lokey`, `hikey`, and `pitch_keycenter` together |
| `pitch_keycenter` | 0-127 | Key playing the sample at original pitch |
| `pitch_keytrack` | int | Key tracking amount |
| `lovel` / `hivel` | 0-127 | Velocity range triggering the region |
| `offset` / `end` | samples | Sample window start and exclusive end frame |
| `default_path` | path prefix | `<control>` prefix for later sample paths |

### Amplifier envelope

`ampeg_delay`, `ampeg_start`, `ampeg_attack`, `ampeg_hold`,
`ampeg_decay`, `ampeg_sustain` (0-100 percent), `ampeg_release`, plus
`ampeg_vel2attack`, `ampeg_vel2decay`, `ampeg_vel2release`,
`ampeg_key2attack`, `ampeg_key2release`. Regions without an `ampeg_*`
block play with a 5 ms attack and 200 ms release. Pitch and filter
envelopes (`pitcheg_*` with depth, `fileg_*` with depth) are stored.

```sfz
<region>
sample=cello_c3.wav lokey=36 hikey=48 pitch_keycenter=48
ampeg_attack=0.01 ampeg_sustain=80 ampeg_release=0.35
```

### Filter, LFO, EQ

| Group | Opcodes |
|-------|---------|
| Filter | `fil_type`, `cutoff`, `resonance`, `fil_keytrack`, `fil_keycenter`, `fil_veltrack`, `fil_random` |
| LFO 1 / LFO 2 | `delay`, `fade`, `freq`, `volume`, `amplitude`, `pitch`, `filter`, `pan`, `volume_smooth`, `wave`, `freq_wave` (plus legacy `amp_lfo_*`, `pitch_lfo_*`, `fil_lfo_*` onto LFO 1) |
| EQ 1-3 | `freq`, `bw`, `gain`, `veltrack` |

### Round-robin

`seq_length` (default 1, off) sets the group step count;
`seq_position` (1-based) sets the region's step. Regions sharing a
length and key range rotate per MIDI key on independent counters.

```sfz
<region>
sample=snare_1.wav lokey=40 hikey=40 pitch_keycenter=40 seq_length=4 seq_position=1
<region>
sample=snare_2.wav lokey=40 hikey=40 pitch_keycenter=40 seq_length=4 seq_position=2
<region>
sample=snare_3.wav lokey=40 hikey=40 pitch_keycenter=40 seq_length=4 seq_position=3
<region>
sample=snare_4.wav lokey=40 hikey=40 pitch_keycenter=40 seq_length=4 seq_position=4
```

Repeated hits walk 1-2-3-4-1. Lone matched members play through
without moving the counter.

### Random selection

`lorand` / `hirand` (0.0-1.0, defaults 0.0/1.0) admit a region only
when the per-note roll falls inside. One roll per note-on filters
before key, velocity, and sequence matching. Inverted windows swap at
parse time.

```sfz
<region>
sample=violin_soft_c4.wav lokey=60 hikey=60 pitch_keycenter=60 lovel=1 hivel=127 lorand=0.0 hirand=0.6
<region>
sample=violin_loud_c4.wav lokey=60 hikey=60 pitch_keycenter=60 lovel=1 hivel=127 lorand=0.4 hirand=1.0
```

Rolls in 0.4-0.6 trigger both layers; elsewhere exactly one plays.
Rolls come from a fixed-seed LCG, so renders are deterministic.

### note_polyphony

`note_polyphony` (integer, default 0 = unlimited, inherits
global/group/region) caps simultaneous voices per MIDI pitch. Past the
limit the oldest voice for that pitch fades over 480 frames (~10 ms)
and frees while the newcomer starts fresh. Unlimited retriggers of one
region fade the old instance the same way so a sample never choruses
against itself.

### Crossfade layers

`xfin_lokey` / `xfin_hikey`, `xfin_lovel` / `xfin_hivel`,
`xfout_lokey` / `xfout_hikey`, `xfout_lovel` / `xfout_hivel` (integers;
`xfin` defaults 0/0, `xfout` defaults 127/127; equal bounds mean no
fade) blend overlapping layers with equal-power sine/cosine gains so
squared gains sum to 1.0.

```sfz
<region>
sample=piano_soft.wav lokey=60 hikey=60 pitch_keycenter=60 lovel=1 hivel=70 xfout_lovel=60 xfout_hivel=70
<region>
sample=piano_loud.wav lokey=60 hikey=60 pitch_keycenter=60 lovel=60 hivel=127 xfin_lovel=60 xfin_hivel=70
```

Velocity 65 sounds both layers at ~0.707 each. Every matched region
gets its own voice with its own gain; short pools drop the quietest
layers first. Probability filters first and can mute a layer;
round-robin resolves one take per layer from the shared per-key step.

### Loop behavior

`loop_mode` (`no_loop`, `loop_continuous`, `one_shot`;
`one_shot` ignores note-off), `loop_start` / `loop_end` (samples,
overriding the WAV `smpl` chunk). A looping voice blends its last 384
source frames (~8 ms) as an equal-power crossfade between the outgoing
tail and an incoming stream from the loop start. Loops shorter than
384 frames use a hard wrap.

```sfz
<group>
loop_mode=loop_continuous
<region>
sample=cello_c3.wav lokey=36 hikey=48 pitch_keycenter=48 loop_start=800 loop_end=43000
```

### Voice management

16 voices per sampler. Pool exhaustion steals the oldest voice with a
240-frame (~5 ms) fade into the pending note. Same-pitch predecessors
fade over 480 frames (~10 ms) and free instead. `off_by` exclusive
groups ride the 10 ms fade path. `polyphony` and `trigger` are stored;
`sw_lokey` / `sw_hikey` / `sw_default` / `sw_last` / `sw_down` /
`sw_up` keyswitches are stored but do not switch articulations.

## Loading real-world libraries

```bash
cp -r ~/Downloads/vsco2-ce ./samples/
./bin/resonata --load-sfz=samples/vsco2-ce/strings.sfz --score=examples/test.json --output=out.wav --verbose
```

```json
{
  "id": "violins",
  "name": "Violins",
  "instrument": {"type": "sampler", "file": "samples/vsco2-ce/strings.sfz"},
  "pan": -0.2,
  "volume": 0.8,
  "notes": [{"time": 0.0, "duration": 2.0, "pitch": 69, "velocity": 0.8}]
}
```

A bundled Windows-authored fixture exercises every loader feature:

```bash
./bin/resonata --load-sfz=samples/windows_authored_library/main.sfz --score=examples/test.json --output=out.wav
```

`#include` directives expand recursively with cycle detection; quoted
and unquoted paths work; separators normalize; lookups share the
case-insensitive cache.

## WAV file requirements

RIFF WAVE with integer PCM 8/16/24/32-bit or IEEE float 32/64-bit,
decoded to float32 in [-1, 1]. Any sample rate (resampled on load).
Mono preferred; stereo downmixes to mono. Loop points come from the
WAV `smpl` chunk, overridable per region.

## Troubleshooting

Sample not found: verify casing, use forward slashes, run with
`--verbose` to see the sanitized path. Clicks at note boundaries:
raise `ampeg_release` (50-200 ms); the fallback envelope uses 200 ms.
Memory exhaustion: samples decode fully on first use, so prefer one
SFZ per instrument. Include cycles abort naming the repeated file.

## Reference

- [Architecture](./ARCHITECTURE.md) — sampler voices and resampling
- [Score DSL](./SCORE_DSL.md) — referencing SFZ files from tracks
- [Cross-Platform](./CROSS_PLATFORM.md) — path and casing rules
