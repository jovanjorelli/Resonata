# Score DSL

Resonata scores are pure JSON documents validated automatically upon intake by
`score.Validate`, which collects every schema and range violation into a single
unified error. Times are expressed in absolute seconds; pitches are MIDI
note numbers (60 = C4, 69 = A440). Parse-time stages execute strictly in order:
transposition, timing offsets, rhythmic swing, and metric accents. Engine
scheduling then applies breath pauses, sustain-pedal moves, and per-note payloads.

Input files are protected by a 64 MiB maximum size limit (`maxScoreFileSize`).

## Metadata fields

| Field | Type | Default | Range | Description |
|-------|------|---------|-------|-------------|
| `title` | string | required | non-empty | Piece title |
| `bpm` | number | required | (0, 1000] | Tempo; drives delay subdivisions, swing beats, humanize timing |
| `time_signature` | string | required | `"N/D"` positive integers | Meter, e.g. `"4/4"` |
| `transpose` | integer | 0 | semitones | Global pitch shift added to every track |
| `swing` | number | 0.0 | [0, 1] | Global eighth-note swing; 1.0 is full triplet feel |
| `breath` | number | 0.0 | milliseconds >= 0 | Global pause inserted between differing non-zero phrases |

`bpm` sets tempo without altering absolute note times. `transpose` adds to each
track's individual transpose value; resulting pitches must stay within MIDI 0–127.
`swing` delays off-beat eighth notes by `swing * beat / 3`. `breath` shifts new
phrases and all subsequent events later in time; transitions involving phrase 0
take no pause.

## Track fields

| Field | Type | Default | Range | Description |
|-------|------|---------|-------|-------------|
| `id` | string | required | unique, non-empty | Track identifier |
| `name` | string | display name | any string | Human-readable label |
| `instrument` | object | required | see below | Sound source definition |
| `pan` | number | 0.0 | [-1, 1] | Stereo position, constant-power |
| `volume` | number | required | [0, 1] | Linear gain |
| `transpose` | integer | 0 | semitones | Per-track shift added to global transpose |
| `swing` | number | 0.0 | [0, 1] | Per-track swing; non-zero overrides global swing |
| `vibrato` | object | absent | `{rate, depth}` | Track pitch LFO unless a note overrides it |
| `expression` | number | full volume | [0, 1] | Track dynamic gain unless a note overrides it |
| `breath` | number | 0.0 | milliseconds >= 0 | Per-track phrase pause; non-zero overrides global breath |
| `room` | string/object | absent | preset or `{size, damping, width}` | Track acoustic space; absent uses master reverb |
| `reverb_send` | number | 0.0 | [0, 1] | Send level into the track's room bus |
| `eq` | object | absent | see below | Parametric sculpt applied before sends |
| `delay` | object | absent | see below | Independent per-track stereo echo |
| `pedal` | array | absent | `[{time, down}]` | Sustain-pedal (CC64) moves; absent means never pressed |
| `notes` | array | empty allowed | note events | Note sequence; at least one track required per score |

While the sustain pedal is held (`down: true`, CC64 value >= 64), note-offs
defer voice release so notes ring naturally; releasing the pedal (`down: false`)
stops every held voice. Moves apply in time order per track.

## Sustain Pedal (CC64)

The track-level `pedal` array holds sustain-pedal moves. While held, released
notes keep ringing naturally like a real acoustic piano, harp, or string section
instead of cutting off at written note-off; releasing the pedal stops every
pending-release voice at once. Each move is `{"time": seconds >= 0, "down": true|false}`.
Sampler instruments implement the `PedalVoicer` interface; procedural voices
(ocarina, synth) accept pedal moves safely without interruption.

```json
{
  "metadata": {"title": "Pedaled Grand", "bpm": 120, "time_signature": "4/4"},
  "tracks": [
    {
      "id": "keys",
      "instrument": {"type": "sampler", "file": "samples/piano.sfz"},
      "volume": 0.9,
      "pedal": [{"time": 0.0, "down": true}, {"time": 2.0, "down": false}],
      "notes": [{"time": 0.0, "duration": 0.2, "pitch": 60, "velocity": 0.9}]
    }
  ]
}
```

## Note event fields

| Field | Type | Default | Range | Description |
|-------|------|---------|-------|-------------|
| `time` | number | required | finite seconds >= 0 | Start time |
| `duration` | number | required | finite seconds > 0 | Length; staccato releases at half length |
| `pitch` | integer | required | MIDI 0-127 after transposition | Note pitch number |
| `velocity` | number | required | [0, 1] | Attack velocity intensity |
| `timing_offset_ms` | number | 0.0 | milliseconds | Deliberate nudge; positive delays, clamped into score |
| `accent` | number | 1.0 | multiplier | Velocity emphasis multiplier (clamped to 1.0) |
| `attack` | number | instrument | seconds >= 0 | Attack time override |
| `decay` | number | instrument | seconds >= 0 | Decay time override |
| `release` | number | instrument | seconds >= 0 | Release time override |
| `phrase_id` | integer | 0 | integer >= 0 | Phrase group driving breath pauses |
| `vibrato` | object | track/absent | `{rate, depth}` | Pitch LFO overriding track vibrato |
| `expression` | number | track/full | [0, 1] | Dynamic gain overriding track expression |
| `articulation` | object | absent | `staccato`, `legato`, `tenuto` | Playing style articulation |

## Instrument definition fields

| Field | Type | Default | Range | Description |
|-------|------|---------|-------|-------------|
| `type` | string | required | `ocarina`, `synth`, or `sampler` | Instrument synthesis type |
| `file` | string | required for sampler | SFZ path | Sample library location |
| `parameters` | map | absent | documented keys | Instrument-specific parameters |

- **Ocarina parameters**: `breath_noise` 0.0–1.0, `vibrato_rate` 1–10 Hz, `vibrato_depth` 0–0.1, `brightness` 0.0–1.0.
- **Sampler parameters**: `master_volume` 0.0–1.0, `attack` 0.0005–2 s, `release` 0.005–10 s.
- **Synth parameters**: `waveform1`, `waveform2` 0/1/2 (saw/square/sine), `cutoff` 20–20000 Hz, `resonance` 0.0–1.0, `attack` 0.0005–2 s, `decay` 0.001–2 s, `sustain` 0.0–1.0, `release` 0.005–10 s, `detune` -12–12 semitones between oscillators.

## Delay fields

| Field | Type | Default | Range | Description |
|-------|------|---------|-------|-------------|
| `mode` | string | `stereo` | `stereo`, `pingpong` (or `ping-pong`/`ping_pong`) | Echo routing topology |
| `subdivision` | string | `1/4` | `1/1`, `1/2`, `1/4`, `1/8`, `1/8d`, `1/16`, `1/16t` | Musical tap length |
| `seconds` | number | 0.0 | [0, 4] | Manual tap time overriding subdivision |
| `feedback` | number | 0.0 | [0, 0.98] | Loop regeneration gain |
| `damping_hz` | number | 4000.0 | [0, 100000), 0 selects 4000 | Feedback lowpass cutoff |
| `wet` | number | 0.0 | [0, 1] | Echo return mix; 0 renders exact dry |

Each track with `delay` owns an independent echo instance. When `lengthSamples <= 0`, dry audio passes through with zero attenuation.

## EQ fields

| Field | Type | Default | Range | Description |
|-------|------|---------|-------|-------------|
| `hpf` | number | 0.0 | 0 (off) or (0, 100000) Hz | High-pass cutoff |
| `eq_low` | object | off | `{freq, gain, q}` | Low shelf |
| `eq_mid` | object | off | `{freq, gain, q}` | Mid peak |
| `eq_high` | object | off | `{freq, gain, q}` | High shelf |

Band fields: `freq` in (0, 100000) Hz, `gain` in [-36, 36] dB, `q` in [0, 50] (0 selects band default).
Chain order: high-pass, low shelf, mid peak, high shelf. All biquad sections feature anti-denormal flush to zero ($< 10^{-25}$) and `NaN`/`Inf` sanitization.

## Room fields

A track `room` is either a preset string (`cathedral`, `hall`, `chapel`, `room`, `plate`, `none`) or a custom object with `{size, damping, width}` in [0, 1]. `none` renders dry. Tracks sharing identical room settings share an FDN reverb instance; past eight distinct spaces, least-used rooms merge into their nearest neighbor with accumulated usage counters.

## Complete example

```json
{
  "metadata": {"title": "Orchestral Showcase", "bpm": 120, "time_signature": "4/4", "transpose": 2, "swing": 0.0, "breath": 80.0},
  "tracks": [
    {
      "id": "flute",
      "name": "Solo Flute",
      "instrument": {"type": "ocarina", "parameters": {"breath_noise": 0.3, "vibrato_rate": 5.5, "vibrato_depth": 0.02, "brightness": 0.9}},
      "pan": -0.2,
      "volume": 0.8,
      "transpose": -5,
      "swing": 0.0,
      "vibrato": {"rate": 5.5, "depth": 0.04},
      "expression": 0.7,
      "breath": 0.0,
      "room": "hall",
      "reverb_send": 0.25,
      "eq": {"hpf": 80.0, "eq_low": {"freq": 200.0, "gain": -2.0, "q": 1.0}, "eq_mid": {"freq": 800.0, "gain": -3.0, "q": 2.5}, "eq_high": {"freq": 8000.0, "gain": 1.5, "q": 0.8}},
      "delay": {"mode": "pingpong", "subdivision": "1/8d", "seconds": 0.0, "feedback": 0.45, "damping_hz": 3500.0, "wet": 0.3},
      "notes": [
        {"time": 0.0, "duration": 1.0, "pitch": 60, "velocity": 0.8, "timing_offset_ms": 2.0, "accent": 1.2, "attack": 0.1, "decay": 0.05, "release": 0.4, "phrase_id": 1, "vibrato": {"rate": 6.0, "depth": 0.05}, "expression": 0.9, "articulation": {"type": "legato"}}
      ]
    }
  ]
}
```

## Validation errors

`score.Validate` aggregates all violations across the entire document into one unified error message. Duplicate track IDs, missing mandatory fields, negative durations, out-of-range pitch values, and non-existent sample files are reported simultaneously with exact track and note identifiers.
