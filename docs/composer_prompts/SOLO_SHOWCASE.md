# Solo Showcase Prompt

Generate a solo instrument showcase as a Resonata JSON score. Output
**only** the JSON document — no prose, no fences, no comments. Times
are seconds (`beat × 60 / bpm`); pitches are MIDI (60 = C4).

## Forces

- One featured instrument: ocarina, flute, violin, or cello character.
  Use `ocarina` type (procedural, no files) with section-appropriate
  parameters, or `sampler` with an existing SFZ `file`.
- Small accompaniment: one or two quiet lines (low strings pad,
  piano-style sampler chords)

## Balance

- Solo instrument prominent: volume 0.9, centered (pan 0.0)
- Accompaniment quiet: volume 0.3–0.5, panned gently off-center
- Solo carries the melody with `legato` phrasing and breath rests;
  accompaniment sustains or plays sparse chords
- Solo reverb 0.6–0.8; accompaniment reverb 0.2–0.4
- Optional single ping-pong echo on the solo (`1/8d`, `feedback`
  ≤ 0.5, `wet` 0.25–0.35)
- If the solo uses a sampler library with round-robin takes, repeated
  phrases vary on their own — no score changes needed

## Duration and ranges

- Target duration: 1–2 minutes
- Solo range C4–C6 (MIDI 60–84), velocity 0.75–0.95
- All pitches 0–127, velocities 0–1, times ≥ 0 and sorted per track
- Delay `feedback` below 0.98; at most two tracks with `delay`
- Optional `transpose` (global or per-track) to fit the solo range;
  final pitches must stay 0–127
- Feel: `accent` 1.2–1.4 on phrase peaks, `timing_offset_ms` +5–8 to
  linger before breath rests, `swing` 0.0 unless the piece swings
- Envelopes: `attack` 0.2–0.4 for a singing legato entrance, short
  `release` where the accompaniment must stay out of the way
- Solo `vibrato` (rate 5.5–6.5, depth 0.04–0.06) on held notes, straight
  tone on runs; `expression` swells toward each phrase peak
- Phrasing is the feature: `phrase_id` per phrase with `breath`
  80–150 before each entrance; solo in `"room"`, accompaniment in
  `"hall"` for contrast

## Complete example

```json
{
  "metadata": {"title": "Solo Spotlight", "bpm": 80, "time_signature": "3/4"},
  "tracks": [
    {
      "id": "solo_violin",
      "name": "Solo Violin",
      "instrument": {"type": "ocarina", "parameters": {"brightness": 0.75, "vibrato_rate": 5.5, "vibrato_depth": 0.025, "breath_noise": 0.3}},
      "pan": 0.0,
      "volume": 0.9,
      "reverb_send": 0.7,
      "delay": {"mode": "pingpong", "subdivision": "1/8d", "feedback": 0.45, "damping_hz": 3500.0, "wet": 0.3},
      "notes": [
        {"time": 0.0, "duration": 1.5, "pitch": 72, "velocity": 0.85, "articulation": {"type": "legato"}},
        {"time": 2.25, "duration": 1.5, "pitch": 76, "velocity": 0.9, "articulation": {"type": "legato"}},
        {"time": 4.5, "duration": 3.0, "pitch": 79, "velocity": 0.9, "articulation": {"type": "legato"}}
      ]
    },
    {
      "id": "pad",
      "name": "String Pad",
      "instrument": {"type": "ocarina", "parameters": {"brightness": 0.35, "vibrato_rate": 3.5, "breath_noise": 0.3}},
      "pan": -0.2,
      "volume": 0.4,
      "reverb_send": 0.3,
      "notes": [
        {"time": 0.0, "duration": 7.5, "pitch": 48, "velocity": 0.6},
        {"time": 0.0, "duration": 7.5, "pitch": 55, "velocity": 0.6}
      ]
    }
  ]
}
```
