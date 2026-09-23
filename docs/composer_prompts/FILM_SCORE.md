# Film Score Cue Prompt

Generate a cinematic film score cue as a Resonata JSON score. Output
**only** the JSON document — no prose, no fences, no comments. Times
are seconds (`beat × 60 / bpm`); pitches are MIDI (60 = C4). Resonata
has two instrument types: `ocarina` (procedural, no files) and
`sampler` (needs an existing SFZ `file`).

## Forces

- Full orchestra: strings, woodwinds, brass, percussion (see the epic
  template for section balance and panning)
- Synthesizer pads if desired: low `brightness` (0.2–0.3), slow
  `vibrato_rate` (2–3), long sustained notes

## Dynamics and arc

- Dramatic dynamics with wide volume range: 0.3 (whispers) to 1.0
  (full tutti)
- The cue must have a clear emotional arc across four phases:
  1. Quiet opening: sparse high strings or solo wind, velocity 0.4–0.55
  2. Building tension: add low strings and brass swells, rising
     velocities, denser rhythms
  3. Climax: full ensemble, forte velocities 0.85–1.0, percussion hits
     (round-robin SFZ takes keep repeated hits alive automatically)
  4. Resolution: thin back to solo or high strings, long final chord
- Mark the arc with velocity ramps over 4–8 beats, not sudden jumps
- Reverb grows with the arc: 0.2 opening, up to 0.7 at climax
- One featured echo (ping-pong `1/8d`) on the climax voice only

## Duration and ranges

- Target duration: 1–4 minutes
- All pitches 0–127, velocities 0–1, times ≥ 0 and sorted per track
- Delay `feedback` below 0.98; at most eight tracks with `delay`
- Reprise the cue in another key with `transpose` (global or per-track);
  final pitches must stay 0–127
- Feel: `timing_offset_ms` +5–10 to linger on resolution tones,
  `accent` 1.3 on climax hits, `swing` 0.0 (straight cinematic time)
- Envelopes: long `attack` on opening strings, sharp `attack` on
  climax stabs, `release` 1.5–2.0 on the final resolving chord
- Swell `expression` 0.6 → 0.9 with the arc; gentle `vibrato` on the
  resolution solo, straight time on stabs
- Mark the four phases with `phrase_id` 1–4 and `breath` 60–100
  between them; keep the climax in `"cathedral"`, dry the heartbeat
  percussion with `"none"`

## Complete example (opening and first swell)

```json
{
  "metadata": {"title": "Cue Opening", "bpm": 72, "time_signature": "4/4"},
  "tracks": [
    {
      "id": "high_strings",
      "name": "High Strings",
      "instrument": {"type": "ocarina", "parameters": {"brightness": 0.4, "vibrato_rate": 4.0, "breath_noise": 0.3}},
      "pan": -0.1,
      "volume": 0.5,
      "reverb_send": 0.2,
      "notes": [
        {"time": 0.0, "duration": 3.3, "pitch": 76, "velocity": 0.45, "articulation": {"type": "legato"}},
        {"time": 3.3, "duration": 3.3, "pitch": 79, "velocity": 0.55, "articulation": {"type": "legato"}}
      ]
    },
    {
      "id": "low_strings",
      "name": "Low Strings",
      "instrument": {"type": "ocarina", "parameters": {"brightness": 0.3, "vibrato_rate": 3.0, "breath_noise": 0.35}},
      "pan": 0.1,
      "volume": 0.55,
      "reverb_send": 0.3,
      "notes": [
        {"time": 3.3, "duration": 3.4, "pitch": 45, "velocity": 0.6, "articulation": {"type": "legato"}},
        {"time": 6.7, "duration": 3.3, "pitch": 43, "velocity": 0.7, "articulation": {"type": "legato"}}
      ]
    }
  ]
}
```
