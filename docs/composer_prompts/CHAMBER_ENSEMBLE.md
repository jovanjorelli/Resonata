# Chamber Ensemble Prompt

Generate a small chamber piece as a Resonata JSON score. Output
**only** the JSON document — no prose, no fences, no comments. Times
are seconds (`beat × 60 / bpm`); pitches are MIDI (60 = C4). Resonata
has two instrument types: `ocarina` (procedural, no files) and
`sampler` (needs an existing SFZ `file`).

## Ensemble

- String quartet or quintet: two violins, viola, cello, optional
  contrabass
- One or two woodwinds (flute and/or clarinet character)
- No brass, no percussion
- Optional piano via `sampler` with an existing SFZ file

## Dynamics and space

- Intimate dynamics: strings volume 0.5–0.7, winds 0.4–0.6, piano
  0.5–0.7
- Less reverb, more dry signal: `reverb_send` 0.1–0.3
- Close panning: keep all tracks within -0.4 to 0.4
- Gentle EQ: high-pass strings at 60–80 Hz to remove rumble
- At most two tracks with `delay`, `feedback` below 0.6
- Repeated detached figures vary automatically when the SFZ library
  provides round-robin takes — no score changes needed

## Duration and ranges

- Target duration: 1–3 minutes
- All pitches 0–127, velocities 0–1, times ≥ 0 and sorted per track
- Articulations: `legato` for lines, `tenuto` for emphasis,
  `staccato` only for light detached figures
- Optional `transpose` (global or per-track) for key variants; final
  pitches must stay 0–127
- Feel: intimate and straight — `swing` 0.0, tiny `timing_offset_ms`
  (±2–4) for breath-like give between phrases, `accent` 1.1–1.2 on
  first-beat entrances
- Envelopes: gentle `attack` 0.1–0.3 on sustained lines, short
  `release` on detached figures
- Light `vibrato` (rate ~5, depth ~0.03) on held string tones, none on
  detached figures; breathe `expression` 0.65 → 0.85 across phrases
- One `phrase_id` per musical sentence with `breath` 50–80; seat the
  ensemble in a small `"room"`, guests in `"hall"`

## Complete example

```json
{
  "metadata": {"title": "Chamber Miniature", "bpm": 84, "time_signature": "4/4"},
  "tracks": [
    {
      "id": "violin1",
      "name": "Violin I",
      "instrument": {"type": "ocarina", "parameters": {"brightness": 0.5, "vibrato_rate": 4.5, "breath_noise": 0.3}},
      "pan": -0.25,
      "volume": 0.65,
      "reverb_send": 0.25,
      "notes": [
        {"time": 0.0, "duration": 1.4, "pitch": 76, "velocity": 0.7, "articulation": {"type": "legato"}},
        {"time": 1.4, "duration": 1.4, "pitch": 74, "velocity": 0.65}
      ]
    },
    {
      "id": "cello",
      "name": "Cello",
      "instrument": {"type": "ocarina", "parameters": {"brightness": 0.35, "vibrato_rate": 3.5, "breath_noise": 0.3}},
      "pan": 0.25,
      "volume": 0.6,
      "reverb_send": 0.2,
      "notes": [
        {"time": 0.0, "duration": 2.8, "pitch": 48, "velocity": 0.65, "articulation": {"type": "legato"}}
      ]
    }
  ]
}
```
