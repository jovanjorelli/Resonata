# Architecture

This document describes Resonata's internal architecture for developers
and contributors. For usage, see [CLI Reference](./CLI_REFERENCE.md);
for the score format, see [Score DSL](./SCORE_DSL.md).

## Audio pipeline graph

```text
SCORE (JSON DSL)
  | Intake: 64 MiB size limit, parse JSON, auto-validate with score.Validate
  | Apply: transposition, timing offsets, swing, accents
  v
ENGINE CORE: frame-accurate on/off events (note-offs first on shared
frames); breath shifts per track; staccato releases at half length
  | dispatch with per-note payload (envelope, vibrato, expression)
  v
+---------------------------------------------------------------+
| PER-TRACK CHAIN (per instrument voice, then strip)            |
|  voice: sample/oscillator -> loop crossfade -> vibrato LFO -> |
|    ADSR envelope -> velocity x expression x crossfade gain    |
|    -> noise gate (sampler, when enabled)                      |
|  strip: EQ sculpt -> pan/volume -> delay (own echo instance)  |
|    -> reverb send into the strip's room bus                   |
+---------------------------------------------------------------+
  |
  v
+---------------------------------------------------------------+
| MASTER MIX BUS                                                |
|  sum dry + per-track echo returns                             |
|  per-room FDN reverb returns x master wet                     |
|  tanh soft clipper (or linear clamp in clean mode)            |
|  lookahead limiter, only with --limiter                       |
|  mono-bass fold, only with --mono-bass                        |
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

### Anti-denormal flush to zero (FTZ)

Recursive IIR filters (RBJ biquad sections in `pkg/dsp/filter.go` and
`pkg/dsp/eq.go`), FDN feedback delay lines (`pkg/dsp/reverb.go`), and
stereo delay damping lines (`pkg/dsp/delay.go`) flush decaying signals
below $10^{-25}$ to zero:

```go
if math.Abs(y) < 1e-25 {
    y = 0
}
```

This prevents floating-point numbers from entering the IEEE 754
subnormal (denormal) range ($< 10^{-308}$ in float64 or $< 10^{-38}$
in float32), which would otherwise cause severe x86 CPU microcode
traps and 10–20× performance degradation during decaying reverb tails
and silent gaps.

## Concurrency and resilience model

Sequential rendering is single-goroutine: no concurrency in the audio
path, which removes race conditions by construction and keeps the
zero-allocation guarantee auditable. The CLI adds only a buffered
progress reporter on stderr. Batch renders files sequentially in one
process, each file with an isolated engine.

With `--parallel`, track voicing fans out across a shared worker pool
(one goroutine per logical CPU, started once per process via
`sync.Once`) while the master sum still runs sequentially in fixed
track order: each track voices only its own instrument, scratch buffer,
and bus, so concurrent tracks never share memory, and the sequential
sum keeps parallel renders byte-identical to sequential ones.

```text
Worker Pool (numCPU workers)
   Track 0 ---> [ Worker A: instrument.Process ] ---\
   Track 1 ---> [ Worker B: instrument.Process ] ----+--> Sync Barrier (parWg.Wait())
   Track 2 ---> [ Worker C: instrument.Process ] ---/           |
                                                                v
                                              Sequential Sum (m.sumTrackBus in order)
```

### Worker panic isolation and error propagation

In parallel mode, worker горутины operate under strict panic isolation:
1. **Buffer Zeroing**: Before invoking `tr.instrument.Process`, the
   track buffer `tr.buffer[:2*n]` is cleared with zeroes. If a panic
   occurs, stale audio from previous blocks is never re-mixed into the
   master output, preventing audio repeat glitches.
2. **Panic Recovery**: Each worker catches panics with `defer recover()`.
   The recovered panic is recorded into `m.parErr` via `errors.Join`
   protected by `m.parErrMu`, ensuring diagnostic information from
   concurrent panics is never lost.
3. **Synchronization & Propagation**: `parWg.Done()` is guaranteed via
   defer. After `m.parWg.Wait()`, `m.Err()` reports the aggregated
   errors to `e.eng.Err()` and `OfflineRenderer.Err()`, cleanly
   halting the render process with exit code 1.

## Score intake pipeline

1. **Size check and decoding**: Input files are read through an
   `io.LimitReader` enforcing a 64 MiB maximum limit (`maxScoreFileSize`).
   Scores are parsed from JSON into the `Score` model with
   `DisallowUnknownFields()` for strict validation.
2. **Automatic validation**: `decodeScore` invokes `score.Validate(&s)`
   immediately upon unmarshaling. Every violation (invalid pitch bounds,
   negative durations, non-positive BPM, duplicate track IDs) is
   collected into a unified multi-line error.
3. **Parse-time transformations**: Transposition is applied globally
   and per-track; timing offsets (`timing_offset_ms`), swing, and
   metric accents adjust note velocities and timestamps.
4. **Engine scheduling**: Breath pauses (`breath`) shift phrase starts
   per track without truncating the output file (`totalFrames` derives
   from `lastEventFrame` plus tail). Notes become frame-accurate
   on/off events with per-note payloads (ADSR overrides, vibrato,
   expression). Sustain-pedal (CC64) events are queued before notes on
   shared frames.
5. **Room bus allocation**: Distinct track acoustic spaces merge past
   eight spaces (least-used-first into the nearest survivor), each
   driving an independent FDN reverb instance.

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
   legacy path (`off_by`, auto-fade, general steal); crossfade sets
   start free voices and drop the quietest layers first.

## Sampler voice lifecycle and loop isolation

States: idle -> active (attack/decay/sustain) -> releasing
(note-off, `off_by`, or cutoff) -> idle. A held sustain pedal defers
the note-off transition: the voice stays active with `pedalHeld` set
until pedal-up releases it.

### Voice stealing hierarchy

When all 16 polyphonic voices are busy, `allocVoice` applies a
strict priority search to avoid dropping queued notes:
1. Releasing voices that are not currently undergoing a steal fade
   (`!v.isStealing`), choosing the oldest release timestamp.
2. Active voices that are fading to silence without a pending note
   (`v.stealFree == true`).
3. Active sounding voices (`!v.isStealing`), choosing the oldest voice
   and fading it over 240 frames (~5 ms) while the incoming note waits.
4. Absolute fallback to the oldest voice.

Predecessor voices on the same pitch fade over 480 frames (~10 ms)
via auto-fade.

### Isolated sample data loops

When regions override sample loop points (`loop_start` or `loop_end`),
the sampler clones the cached `*SampleData` structure before
modifying loop bounds. This ensures independent regions sharing the
same underlying audio file do not mutate each other's loop boundaries.

## Path resolution and security

The SFZ path resolver (`pkg/instruments/sampler/path_resolver.go`)
provides sandboxed, cross-platform file access:
- **Symlink traversal protection**: `isWithinBase` verifies that
  canonical paths (`filepath.EvalSymlinks`) remain anchored within the
  library's root directory (`realBase`), preventing directory
  traversal escapes via symbolic links.
- **Drive letter normalization**: Windows-authored paths with drive
  letters (e.g. `C:\samples\sound.wav`) are stripped of their `C:`
  prefix and backslashes converted to forward slashes on Linux/macOS.
- **Concurrent case-insensitive lookup**: On case-sensitive systems,
  directory trees are walked matching lowercase names. Resolved paths
  are cached in a thread-safe map using `atomic.Uint64` hit/miss
  counters, eliminating mutex contention across parallel worker threads.

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

## Synth voice

The synth (`pkg/instruments/synth`) is a 16-voice polyphonic
subtractive voice needing no sample files: two oscillators (saw,
square, or sine each, with semitone detune on the second) through a
resonant one-pole lowpass under a per-voice ADSR envelope. With all
voices busy the oldest sounding voice is stolen immediately.
Per-note envelope overrides and expression apply; vibrato payloads
are accepted and ignored.

## DSP formulas and numerical safety

### Cubic Hermite (Catmull-Rom) resampling
Evaluated by Horner's method on FMA chains with empty-slice guards:
```text
c0 = p1,  c1 = (p2 - p0)/2
c2 = p0 - 5p1/2 + 2p2 - p3/2,  c3 = 3p1/2 - p0/2 + p3/2 - 3p2/2
y  = ((c3*x + c2)*x + c1)*x + c0
```

### Equal-power crossfade
Used for loop seams and velocity/key layer zones:
```text
out_gain = cos(t * pi/2),  in_gain = sin(t * pi/2)
```

### Biquad filters (RBJ Cookbook Direct Form I)
```text
y[n] = b0*x[n] + b1*x[n-1] + b2*x[n-2] - a1*y[n-1] - a2*y[n-2]
```
Inputs, outputs, and setter parameters (`SetFrequency`, `SetQ`,
`SetGainDB`) are guarded against `NaN`/`Inf`. Any non-finite values
reset filter delay states to zero.

### Master soft clipper
Threshold $t = 0.9$ (clamped to prevent division by zero when $t \to 1$):
```text
y = x                                            |x| <= t
y = sign(x)*(t + (1-t)*tanh((|x|-t)/(1-t)))      |x| > t
```
In clean mode (`--saturation=clean`), signal passes linearly with a
hard clamp at ±1.0 and zero tanh coloration.

### Lookahead limiter
-0.1 dBFS ceiling with 5 ms lookahead delay ring buffer. The auto-gain
loudness detector is guarded against negative or zero mean-square
values, and output samples pass through safe ceiling clamping that
mutes any non-finite values to zero.

## Stereo delay math

Two ring lines share one tap length with a 1-pole lowpass in feedback
and stereo or cross-coupled ping-pong routing. BPM-synced length:

```text
samples = (60 / BPM) * subdivision * fs
```

with `1/1` = 4.0, `1/2` = 2.0, `1/4` = 1.0, `1/8` = 0.5, `1/8d` =
0.75, `1/16` = 0.25, `1/16t` = 1/3; `seconds` overrides up to 4 s.
Every track with `delay` owns a fully independent instance; when
`lengthSamples <= 0`, dry audio passes through transparently with
zero attenuation.

## WAV encoder and decoder specifications

- **Header validation**: Decoders bound chunk sizes against available
  stream lengths to prevent unconstrained memory allocations and OOM
  panics on malformed RIFF headers.
- **Container bit depths**: Multi-channel layouts and 32-bit float
  utilize `WAVE_FORMAT_EXTENSIBLE`. 24-bit samples packed into 32-bit
  containers retain their 4-byte block alignment and normalize over
  $2^{31}$.
- **Surround mask**: 8-channel layouts apply the standard 7.1 surround
  mask `0x63F` (Front Left/Right/Center, LFE, Back Left/Right,
  Side Left/Right).

## Extending the engine

1. Implement the `Instrument` interface in
   `pkg/instruments/<type>/` (`Process`, `NoteOn`, `NoteOff`,
   `SetParameters`); optionally implement `PedalVoicer` (`SetPedal(bool)`).
2. Register the type in `newInstrument` (`pkg/engine/engine.go`) and
   document it in [Score DSL](./SCORE_DSL.md).
3. Keep `Process` allocation-free with constructor-owned state and
   prove it with an `AllocsPerRun` test.
