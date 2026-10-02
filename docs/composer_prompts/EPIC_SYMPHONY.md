# Epic Symphony Prompt

Generate a full epic orchestral symphony as a Resonata JSON score.
Output **only** the JSON document — no prose, no fences, no comments.
Times are seconds (`beat × 60 / bpm`); pitches are MIDI (60 = C4).
Resonata has two instrument types: `ocarina` (procedural reference
voice, no files) and `sampler` (needs an existing SFZ `file`). Map each
orchestral section onto one of these two types.

## Required sections

- Strings: violin one, violin two, viola, cello, contrabass
- Woodwinds: flute, oboe, clarinet, bassoon
- Brass: French horn, trumpet, trombone, tuba
- Percussion: timpani, snare, cymbals (short `staccato` hits work well)
- Featured ocarina solo (mandatory, see below)

## Balance guidelines

- Strings are the foundation: volume 0.7–0.9, sustained chords
- Woodwinds add color: volume 0.6–0.8, counterpoint lines
- Brass provides power: volume 0.5–0.8, fanfares (lower when supporting)
- Percussion is rhythmic: volume 0.6–0.9, complements without dominating.
  Prefer SFZ libraries with round-robin takes for snare and cymbal
  ostinati — repeated hits cycle automatically with no score changes.

## Panning guidelines

- Violin one -0.4, violin two -0.2, viola 0.1, cello 0.3, contrabass 0.0
- Woodwinds spread -0.5 to 0.5
- Brass centered -0.3 to 0.3
- Percussion spread wide (-0.7 to 0.7)
- Ocarina solo centered at 0.0

## Ocarina solo requirements

- At least 30 seconds of solo material, `legato` articulation
- Centered (pan 0.0), volume 0.8–1.0, range C4–C6 (MIDI 60–84)
- Ping-pong delay for atmosphere (`subdivision` `1/8d`,
  `feedback` ≤ 0.6, `wet` 0.3–0.5)
- Breath rests between phrases; velocity 0.7–0.9

## Duration and ranges

- Target duration: 2–5 minutes
- All pitches 0–127, velocities 0–1, times ≥ 0 and sorted per track
- Delay `feedback` below 0.98; at most eight tracks with `delay`
- Reverb sends: strings 0.5–0.7, woodwinds 0.3–0.5, brass 0.2–0.4,
  percussion 0.1–0.3, solo 0.6–0.9
- Key changes via `transpose` (global in `metadata`, per-track on
  tracks): shifts add in semitones, final pitches must stay 0–127
- Feel: `swing` 0.0 (straight marches) unless a folk dance calls for
  lilt; `accent` 1.2–1.4 on fanfare downbeats, `timing_offset_ms` -3
  to rush pickups into the solo
- Envelopes: `attack` ~0.005 and short `release` on brass/percussion
  hits; `attack` 0.3–0.5 with `release` 1.0+ on the solo's long tones
- Vibrato on the solo and sustained strings (rate 5–6, depth 0.03–0.05),
  never on percussion; swell `expression` 0.7 → 0.9 into climaxes
- Shape the arc in sentences: `phrase_id` per section line with
  `breath` 80–120 between them; seat the solo in `"room"` or `"hall"`
  against a `"cathedral"` tutti

## Complete example (excerpt shape)

```json
{
  "metadata": {"title": "Epic Excerpt", "bpm": 100, "time_signature": "4/4"},
  "tracks": [
    {
      "id": "violin1",
      "name": "Violin I",
      "instrument": {"type": "ocarina", "parameters": {"brightness": 0.45, "vibrato_rate": 4.5, "vibrato_depth": 0.015, "breath_noise": 0.35}},
      "pan": -0.4,
      "volume": 0.85,
      "reverb_send": 0.6,
      "notes": [
        {"time": 0.0, "duration": 2.4, "pitch": 76, "velocity": 0.8, "articulation": {"type": "legato"}},
        {"time": 2.4, "duration": 2.4, "pitch": 74, "velocity": 0.75}
      ]
    },
    {
      "id": "horn",
      "name": "French Horn",
      "instrument": {"type": "ocarina", "parameters": {"brightness": 0.6, "vibrato_rate": 4.0, "breath_noise": 0.3}},
      "pan": -0.2,
      "volume": 0.65,
      "reverb_send": 0.3,
      "notes": [
        {"time": 0.0, "duration": 1.2, "pitch": 55, "velocity": 0.85},
        {"time": 2.4, "duration": 1.2, "pitch": 53, "velocity": 0.8}
      ]
    },
    {
      "id": "timpani",
      "name": "Timpani",
      "instrument": {"type": "ocarina", "parameters": {"brightness": 0.25, "vibrato_depth": 0.0, "breath_noise": 0.4}},
      "pan": 0.0,
      "volume": 0.7,
      "reverb_send": 0.2,
      "notes": [
        {"time": 0.0, "duration": 0.4, "pitch": 43, "velocity": 0.9, "articulation": {"type": "staccato"}},
        {"time": 2.4, "duration": 0.4, "pitch": 41, "velocity": 0.9, "articulation": {"type": "staccato"}}
      ]
    },
    {
      "id": "ocarina_solo",
      "name": "Ocarina Solo",
      "instrument": {"type": "ocarina", "parameters": {"breath_noise": 0.3, "vibrato_rate": 5.5, "vibrato_depth": 0.02, "brightness": 0.9}},
      "pan": 0.0,
      "volume": 0.95,
      "reverb_send": 0.7,
      "delay": {"mode": "pingpong", "subdivision": "1/8d", "feedback": 0.5, "damping_hz": 3500.0, "wet": 0.4},
      "notes": [
        {"time": 0.0, "duration": 6.0, "pitch": 69, "velocity": 0.85, "articulation": {"type": "legato"}},
        {"time": 7.0, "duration": 6.0, "pitch": 72, "velocity": 0.85, "articulation": {"type": "legato"}},
        {"time": 14.0, "duration": 6.0, "pitch": 76, "velocity": 0.9, "articulation": {"type": "legato"}},
        {"time": 21.0, "duration": 6.0, "pitch": 74, "velocity": 0.85, "articulation": {"type": "legato"}},
        {"time": 28.0, "duration": 6.0, "pitch": 72, "velocity": 0.85, "articulation": {"type": "legato"}}
      ]
    }
  ]
}
```
