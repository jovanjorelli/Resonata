# Score DSL

Resonata scores are JSON or YAML documents validated by `score.Validate`,
which collects every violation into one error. Times are absolute
seconds; pitches are MIDI numbers (60 = C4, 69 = A440). Parse-time
stages run in order: transposition, timing offsets, swing, accents.
Engine scheduling then applies breath pauses and per-note payloads.

## Metadata fields

| Field | Type | Default | Range | Description |
|-------|------|---------|-------|-------------|
| `title` | string | required | non-empty | Piece name |
| `bpm` | number | required | (0, 1000] | Tempo; drives delay subdivisions, swing beats, humanize timing |
| `time_signature` | string | required | `"N/D"` positive integers | Meter, e.g. `"4/4"` |
| `transpose` | integer | 0 | semitones | Global pitch shift added to every track |
| `swing` | number | 0.0 | [0, 1] | Global eighth-note swing; 1.0 is full triplet feel |
| `breath` | number | 0.0 | milliseconds >= 0 | Global pause inserted between differing non-zero phrases |

`bpm` does not stretch note times. `transpose` adds to each track
transpose; results must stay in MIDI 0-127. `swing` delays off-beat
eighth notes by `swing * beat / 3`. `breath` shifts a new phrase and
everything after it later; transitions touching phrase 0 take no pause.

## Track fields

| Field | Type | Default | Range | Description |
|-------|------|---------|-------|-------------|
| `id` | string | required | unique, non-empty | Track identifier |
| `name` | string | display name | any string | Human-readable label |
| `instrument` | object | required | see below | Sound source |
| `pan` | number | 0.0 | [-1, 1] | Stereo position, constant-power |
| `volume` | number | required | [0, 1] | Linear gain |
| `transpose` | integer | 0 | semitones | Per-track shift added to the global transpose |
| `swing` | number | 0.0 | [0, 1] | Per-track swing; non-zero overrides the global value |
| `vibrato` | object | absent | `{rate, depth}` | Track pitch LFO unless a note overrides it |
| `expression` | number | full volume | [0, 1] | Track dynamic gain unless a note overrides it |
| `breath` | number | 0.0 | milliseconds >= 0 | Per-track phrase pause; non-zero overrides the global value |
| `room` | string/object | absent | preset or `{size, damping, width}` | Track acoustic space; absent uses the master reverb |
| `reverb_send` | number | 0.0 | [0, 1] | Send level into the track's room |
| `eq` | object | absent | see below | Parametric sculpt applied before sends |
| `delay` | object | absent | see below | Independent per-track echo |
| `notes` | array | empty allowed | note events | The part itself; at least one track required per score |

## Note event fields

| Field | Type | Default | Range | Description |
|-------|------|---------|-------|-------------|
| `time` | number | required | finite seconds >= 0 | Start time |
| `duration` | number | required | finite seconds > 0 | Length; staccato releases at half |
| `pitch` | integer | required | MIDI 0-127 after transposition | Note pitch |
| `velocity` | number | required | [0, 1] | Attack intensity |
| `timing_offset_ms` | number | 0.0 | milliseconds | Deliberate nudge; positive delays, clamps into the score |
| `accent` | number | 1.0 | multiplier | Velocity emphasis; final velocity clamps to [0, 1] |
| `attack` | number | 0.0 | seconds | Per-note attack override; 0 keeps the SFZ value |
| `decay` | number | 0.0 | seconds | Per-note decay override; 0 keeps the SFZ value |
| `release` | number | 0.0 | seconds | Per-note release override; 0 keeps the SFZ value |
| `phrase_id` | integer | 0 | any integer | Phrase tag; 0 means no named phrase |
| `vibrato` | object | absent | `{rate, depth}` | Per-note pitch LFO overriding the track value |
| `expression` | number | track value | [0, 1] | Per-note dynamic gain overriding the track value |
| `articulation` | object | absent | `staccato`, `legato`, `tenuto` | Playing style plus optional params |

## Instrument definition fields

| Field | Type | Default | Range | Description |
|-------|------|---------|-------|-------------|
| `type` | string | required | `ocarina` or `sampler` | Sound source |
| `file` | string | required for sampler | SFZ path | Sample library location |
| `parameters` | map | absent | documented keys | Instrument knobs; unknown keys ignored |

Ocarina parameters: `breath_noise` 0.0-1.0, `vibrato_rate` 1-10 Hz,
`vibrato_depth` 0-0.1, `brightness` 0.0-1.0. Sampler parameters:
`master_volume` 0.0-1.0, `attack` 0.0005-2 s, `release` 0.005-10 s.

## Delay fields

| Field | Type | Default | Range | Description |
|-------|------|---------|-------|-------------|
| `mode` | string | `stereo` | `stereo`, `pingpong` (plus `ping-pong`/`ping_pong` spellings) | Echo routing |
| `subdivision` | string | `1/4` | `1/1`, `1/2`, `1/4`, `1/8`, `1/8d`, `1/16`, `1/16t` (or `whole`...`sixteenth`) | Musical tap length |
| `seconds` | number | 0.0 | [0, 4] | Manual tap time overriding the subdivision |
| `feedback` | number | 0.0 | [0, 0.98] | Loop gain |
| `damping_hz` | number | 4000.0 | [0, 100000), 0 selects 4000 | Feedback lowpass cutoff |
| `wet` | number | 0.0 | [0, 1] | Echo return mix; 0 renders exact dry |
| `send` | number | deprecated | parsed but ignored | Omit in new scores |

Each track with `delay` owns a fully independent echo instance. There
is no shared bus.

## EQ fields

| Field | Type | Default | Range | Description |
|-------|------|---------|-------|-------------|
| `hpf` | number | 0.0 | 0 (off) or (0, 100000) Hz | High-pass cutoff |
| `eq_low` | object | off | `{freq, gain, q}` | Low shelf |
| `eq_mid` | object | off | `{freq, gain, q}` | Mid peak |
| `eq_high` | object | off | `{freq, gain, q}` | High shelf |

Band fields: `freq` in (0, 100000) Hz, `gain` in [-36, 36] dB, `q` in
[0, 50] with 0 selecting the band default. An all-zero band is off.
Chain order: high-pass, low shelf, mid peak, high shelf.

## Room fields

A track `room` is a preset-name string (`cathedral`, `hall`, `room`,
`none`) or an object with `size`, `damping`, `width` each in
[0, 1]. `none` renders the track dry. Tracks sharing one
configuration share one reverb instance; past eight distinct spaces
the least-used merge into their nearest neighbor.

## Vibrato fields

`rate` in Hz (typically 3.0-8.0, default 5.0), `depth` in semitones
(typically 0.01-0.15, default 0.03). Absent means straight tone; a
note value replaces the track value. The sampler LFO skips the attack,
waits 150 ms, then ramps over 100 ms.

## Complete example

```json
{
  "metadata": {"title": "Full", "bpm": 120, "time_signature": "4/4", "transpose": 2, "swing": 0.0, "breath": 80.0},
  "tracks": [
    {
      "id": "flute",
      "name": "Flute",
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

The written pitch 60 renders as 57 (60 + 2 - 5).

## Validation errors

`Validate` collects every violation into one joined error labeled
`tracks[i] "id"`. Transposition overflows name the track, note index,
original and resulting pitch, and both shifts. Unknown articulation
types, empty ids, duplicate ids, missing sampler files, and
out-of-range numerics are all reported together.

## Reference

- [CLI Reference](./CLI_REFERENCE.md) — rendering scores
- [SFZ Guide](./SFZ_GUIDE.md) — sampler instruments
- [Composer Prompt](./COMPOSER_PROMPT.md) — writing musical scores
- [Architecture](./ARCHITECTURE.md) — engine scheduling and effects math
