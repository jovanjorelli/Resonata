# Architecture

This document describes Resonata's internal architecture for developers
and contributors. For usage, see [CLI Reference](./CLI_REFERENCE.md);
for the score format, see [Score DSL](./SCORE_DSL.md).

## Audio pipeline graph

```text
SCORE (JSON/YAML DSL)
  | Parse: transpose, timing offsets, swing, accents; validate
  v
ENGINE CORE: frame-accurate on/off events (note-offs first on shared
frames); breath shifts per track; staccato releases at half length
  | dispatch with per-note payload (envelope, vibrato, expression)
  v
+---------------------------------------------------------------+
| PER-TRACK CHAIN (per instrument voice, then strip)            |
|  voice: sample/oscillator -> loop crossfade -> vibrato LFO -> |
|    ADSR envelope -> velocity x expression x crossfade gain    |
|  strip: EQ sculpt -> pan/volume -> delay (own echo instance)  |
|    -> reverb send into the strip's room bus                   |
+---------------------------------------------------------------+
  |
  v
+---------------------------------------------------------------+
| MASTER MIX BUS                                                |
|  sum dry + per-track echo returns                             |
|  per-room FDN reverb returns x master wet                     |
|  tanh soft clipper                                            |
+---------------------------------------------------------------+
  |
  v
WAV WRITER (PCM 16/24/32 or 32-bit float, EXTENSIBLE as needed)
```

The engine converts every note into a frame-accurate on/off event
pair sorted with note-offs first on shared frames. A 2.5 s tail
extends the render past the last note-off so envelopes and reverb
decay inside the file. Block size defaults to 1024 frames.

## Zero-allocation strategy

The audio loop never allocates heap memory. Voices, envelopes,
per-pitch rosters, trigger scratch, mixer track buses, send-bus pool,
reverb instances, echo ring lines, and LFO state are pre-allocated at
construction or setup; per-block rendering reuses them. Growth (mix
scratch, trigger scratch) happens at most once per size step outside
the steady state. Every hot path carries an `AllocsPerRun` test
asserting zero allocations. One-time `engine.New` event building does
allocate; the per-block loop does not.

```bash
go test -bench=. -benchmem ./pkg/...
# Steady-state audio benchmarks must show: 0 B/op, 0 allocs/op
```

The CLI calls `debug.SetGCPercent(-1)` around the render loop so the
collector cannot pause mid-render, restoring the default schedule
afterwards.

## Concurrency model

Single-goroutine offline renderer: no concurrency in the audio path,
which removes race conditions by construction and keeps the
zero-allocation guarantee auditable. The CLI adds only a buffered
progress reporter on stderr. Batch throughput uses process-level
parallelism.

## Score intake pipeline

1. Parse JSON or YAML into the score model (unknown fields ignored).
2. Apply transposition, timing offsets, swing, and accents; validate
   every field and collect all violations into one error.
3. Engine scheduling: breath pauses shift phrase starts per track,
   notes become on/off frame events with per-note payloads
   (envelope overrides, vibrato, expression).
4. Room collection: distinct track spaces merge past eight,
   least-used-first into the nearest survivor; each gets one FDN.

## Region matching pipeline

On every sampler note-on, in order:

1. Draw one random roll in [0,1) for the event (fixed-seed LCG).
2. Filter regions by `lorand`/`hirand` windows (defaults admit all).
3. Filter by key range and velocity range.
4. Compute each survivor's crossfade gain from its `xfin`/`xfout`
   zones (equal-power sine/cosine, 1.0 outside every zone).
5. Apply sequence filtering: groups resolve one take per layer from
   the per-key counter (advanced once per engaging note-on); lone
   members pass through.
6. Fire the trigger set highest-gain-first: lone triggers take the
   legacy path (off_by, auto-fade, general steal); crossfade sets
   start free voices and drop the quietest layers first.

## Sampler voice lifecycle

States: idle -> active (attack/decay/sustain) -> releasing
(note-off, `off_by`, or cutoff) -> idle. Two fading overlays:

- Stealing: all 16 voices busy, so the oldest voice fades over 240
  frames (~5 ms) while its pending note waits, then repurposes.
- Auto-fade: same-pitch predecessors (past `note_polyphony`, or
  same-region doubles when unlimited) fade over 480 frames (~10 ms)
  and free while the newcomer starts fresh.

A fixed 128-entry roster tracks sounding voices per pitch (8 slots
each, full-scan fallback past the cap). Fading voices stay active
until the fade completes, then free or repurpose inside the render
loop. Crossfade-layer voices are tracked and faded like any other.

## Ocarina reference voice

The ocarina (`pkg/instruments/ocarina`) is a procedural Helmholtz
resonator for pipeline validation, not a production instrument: sine
harmonics plus formant-filtered breath noise under an LFO vibrato,
with a 50 ms attack and 100 ms release breath envelope that accepts
the same per-note overrides as the sampler. Production rendering uses
the SFZ sampler with real samples.

```text
f = (c / 2pi) * sqrt(S / (V * Leff))
```

Each `NoteOn` inverts the formula so the cavity resonates at exactly
the requested MIDI pitch.

## DSP formulas

Cubic Hermite (Catmull-Rom) resampling over taps p0..p3 at fraction x
in [0,1], evaluated by Horner's method on FMA chains:

```text
c0 = p1,  c1 = (p2 - p0)/2
c2 = p0 - 5p1/2 + 2p2 - p3/2,  c3 = 3p1/2 - p0/2 + p3/2 - 3p2/2
y  = ((c3*x + c2)*x + c1)*x + c0
```

Equal-power crossfade at progress t in [0,1] (loop seams, velocity
and key layers):

```text
out_gain = cos(t * pi/2),  in_gain = sin(t * pi/2)
```

Biquad filters follow the RBJ Audio EQ Cookbook (Direct Form I):

```text
y[n] = b0*x[n] + b1*x[n-1] + b2*x[n-2] - a1*y[n-1] - a2*y[n-2]
```

used for lowpass, highpass, bandpass, peak, and shelving sections in
EQ bands, breath formants, and delay damping.

Master soft clipper (threshold t = 0.9):

```text
y = x                                    |x| <= t
y = sign(x)*(t + (1-t)*tanh((|x|-t)/(1-t)))  |x| > t
```

The continuous knee asymptotes to ±1 without hard clipping.

## Stereo delay math

Two ring lines share one tap length with a 1-pole lowpass in feedback
and stereo or cross-coupled ping-pong routing. BPM-synced length:

```text
samples = (60 / BPM) * subdivision * fs
```

with `1/1` = 4.0, `1/2` = 2.0, `1/4` = 1.0, `1/8` = 0.5, `1/8d` =
0.75, `1/16` = 0.25, `1/16t` = 1/3; `seconds` overrides up to 4 s.
Every track with `delay` owns a fully independent instance; `wet: 0`
renders exact dry.

## Master chain order

Per-track EQ sculpts before the mixer, so reverb sends and each
track's echo carry the equalized sound. The master sums dry strips
plus per-track echo returns, adds each room's FDN return scaled by
the master wet level, and finishes with the tanh soft clipper. There
is no master delay bus and no shared reverb input: rooms keep
separate send buses.

## Extending the engine

1. Implement the `Instrument` interface in
   `pkg/instruments/<type>/` (`Process`, `NoteOn`, `NoteOff`,
   `SetParameters`); optionally accept `NoteOnParams` payloads.
2. Register the type in `newInstrument` (`pkg/engine/engine.go`) and
   document it in [Score DSL](./SCORE_DSL.md).
3. Keep `Process` allocation-free with constructor-owned state and
   prove it with an `AllocsPerRun` test.
