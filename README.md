# Resonata

Offline orchestral score renderer in pure Go: reads JSON scores or Standard MIDI Files and renders deterministic WAV audio with zero dependencies and zero CGO.

> [!TIP]
> If Resonata is useful to you, consider starring the repository — it helps others discover the project. Issues and contributions are welcome under the same license terms below.

## Overview

Resonata takes a declarative score (JSON), schedules every note to an exact audio frame, synthesizes each track through SFZ sample libraries, a subtractive synth voice, or a built-in physical-model voice, mixes through per-track EQ, echo, and convolution-free algorithmic reverb, and writes a mastered WAV file. The same input always produces byte-identical output: all randomness uses fixed seeds with deterministic scheduling, and the per-block render loop performs zero heap allocations.

> [!NOTE]
> Clone the repository and build from source as described in [Build](#build).

## Features

- Pure JSON score DSL with automatic schema validation and 64 MiB input guard
- Standard MIDI File import (format 0/1) and export (format 1, 480 PPQ)
- Polyphonic SFZ sampler: cubic Hermite resampling, round-robin, random layers, crossfades, loop crossfade, priority voice stealing with fades, and isolated region loop points
- Ocarina Helmholtz physical-model reference voice for pipeline smoke tests
- Subtractive synth voice: dual oscillators, resonant lowpass, ADSR, no sample files
- Per-track parametric EQ, independent stereo/ping-pong delay, per-room FDN reverb, tanh soft clipper, lookahead mastering limiter, LUFS metering
- Full numerical robustness: complete `NaN`/`Inf` sanitization across all DSP filters and delay lines, with automatic denormal flushing to zero ($< 10^{-25}$) to prevent CPU microcode stalls
- Concurrency hardening: parallel per-track rendering with worker panic isolation (`recover()`), multitrack error aggregation (`errors.Join`), and zero stale audio leakage
- Humanizer with Gaussian micro-timing and metric accents (deterministic seed)
- Sustain pedal (CC64) that holds sampler voices past note-off until release
- WAV output: 16/24/32-bit PCM and 32-bit float; 44100/48000/96000 Hz; mono, stereo, and 7.1 surround layouts (`0x63F` channel mask)
- Headless CLI with batch directory rendering, benchmarking, CPU/heap profiling, and optional parallel per-track rendering with byte-identical output
- Zero external dependencies, zero CGO, fully static binaries

## Build

Requires Go 1.27 or later. No system libraries needed.

```bash
CGO_ENABLED=0 go build -ldflags="-s -w" -o bin/resonata ./cmd/resonata
```

Cross-compile for Windows from Linux:

```bash
CGO_ENABLED=0 GOOS=windows GOARCH=amd64 go build -ldflags="-s -w" -o bin/resonata.exe ./cmd/resonata
```

> [!IMPORTANT]
> `CGO_ENABLED` must be `0`. Any other value breaks the static, dependency-free build the verification script enforces. The output path `bin/resonata` is a convention; any path works.

Measured binary sizes (stripped, `CGO_ENABLED=0`): 3510432 bytes (~3.5 MB) Linux amd64, 3720192 bytes (~3.7 MB) Windows amd64.

## Quick Start

Print the version:

```bash
./bin/resonata --version
```

```
Resonata v3
```

Render the bundled example:

```bash
./bin/resonata --score=examples/simple_score.json --output=out.wav
```

```
Rendered 1 tracks -> out.wav (4.50 s, peak 0.445, 73ms)
```

Render with humanization, a cathedral reverb, and explicit output format:

```bash
./bin/resonata --score=examples/epic_score.json --output=epic.wav --humanize=0.6 --reverb=cathedral --bit-depth=24 --channels=2 --sample-rate=48000
```

> [!TIP]
> Run a small score with `--benchmark` first: it renders 10x in memory and reports the average time, which predicts full-render cost without writing files.

## Demo

**Direct download:** [demo_track.wav](demo/demo_track.wav)

A full 97-second stereo render (27.9 MB, 48 kHz 24-bit) with sustained ensemble dynamics throughout (segment peaks 0.60–0.64, RMS ~0.066, decaying tail in the final seconds). No source score ships with it, so it cannot be re-rendered from the repository; it is included as a listening reference for the renderer's output.

## Sample Libraries and Audio Quality

> [!WARNING]
> **Vintage audio artifacts from third-party samples**
>
> Some free sample libraries (especially Karoryfer: Meatbass, War Tuba) contain:
> - Microphone preamp noise (noise floor -38 to -41 dB)
> - Intentional pitch wobble (wow & flutter) via LFO modulation in SFZ files
> - Tape saturation characteristics baked into samples
>
> Resonata provides three flags to clean up the audio:
> - `--saturation=clean` disables the analog-style soft clipper (default: `tape`)
> - `--reverb-damping=0.0` removes high-frequency cutoff from reverb tails (default: preset damping)
> - `--noise-gate=-60` mutes samples below -60 dB to eliminate background noise (default: disabled)
>
> Example clean render:
> ```bash
> ./bin/resonata --score=examples/simple_score.json --output=out.wav \
>   --saturation=clean \
>   --reverb-damping=0.0 \
>   --noise-gate=-60
> ```
>
> For studio-quality results, use clean sample libraries like VSCO 2 Community Edition or Sonatina Symphonic Orchestra.

## Clean audio rendering

The three cleanup flags combine in a single command:

```bash
./bin/resonata --score=examples/simple_score.json --output=clean.wav \
  --saturation=clean --reverb-damping=0.0 --noise-gate=-60
```

- `--saturation=clean` bypasses the master soft clipper: signal passes linearly up to ±1.0, with a hard clamp at full scale and no tanh coloration.
- `--reverb-damping=0.0` disables air-absorption lowpasses on every reverb line, producing a bright, extended tail that never rolls off high harmonics.
- `--noise-gate=DB` sets a threshold in dBFS (typically `-60` to `-45`); voice output below the threshold mutes before mixing, cutting background hiss from vintage samples during quiet passages.

Together they produce a modern, transparent render from library files you intend to keep; see [docs/SAMPLE_LIBRARIES.md](./docs/SAMPLE_LIBRARIES.md).

## Output Formats

WAV combinations: 16-bit PCM, 24-bit PCM, 32-bit integer PCM, 32-bit IEEE float. `WAVE_FORMAT_EXTENSIBLE` is used for float, 24-bit in 32-bit container formats, and multi-channel layouts. Sample rates: 44100, 48000, 96000. Channel counts: mono (stereo downmixed), stereo, and 8-channel surround (7.1 mask `0x63F`).

```bash
./bin/resonata --score=examples/simple_score.json --output=out16.wav --bit-depth=16 --channels=1 --sample-rate=44100
./bin/resonata --score=examples/simple_score.json --output=outf.wav --bit-depth=32f --channels=2
```

## Performance

Measured on Intel Xeon E5-2640 v3, Go 1.27 (`-benchtime=200x`):

| Benchmark | Result | Allocations |
|-----------|--------|-------------|
| Oscillator (Sine), 1024 samples | 11.7 µs/op | 0 B/op, 0 allocs/op |
| Biquad Filter, 1024 samples | 21.0 µs/op | 0 B/op, 0 allocs/op |
| Lookahead Limiter, 1024 samples | 479.4 µs/op | 0 B/op, 0 allocs/op |
| FDN Algorithmic Reverb, 1 sample | 553.3 ns/op | 0 B/op, 0 allocs/op |
| Stereo Delay, 1024 samples | 51.2 µs/op | 0 B/op, 0 allocs/op |
| Master Mixer, 8 tracks (sequential) | 514.4 µs/op | 0 B/op, 0 allocs/op |
| Master Mixer, 8 tracks (parallel) | 765.6 µs/op | 0 B/op, 0 allocs/op |
| Sampler note-on, random | 368.2 ns/op | 0 B/op, 0 allocs/op |
| Sampler note-on, sequence | 340.7 ns/op | 0 B/op, 0 allocs/op |

The audio loop never allocates heap memory: voices, envelopes, mixer buses, reverb instances, and echo ring lines are pre-allocated at construction, and every hot path carries an allocation test asserting zero. The CLI disables the garbage collector around the render loop (`debug.SetGCPercent(-1)`) so no GC pause can stall a long render. Absolute times vary with hardware; the zero-allocation guarantee does not.

## Technical Notes

- **Limiter lookahead.** With `--limiter`, the 5 ms lookahead buffer delays output by ~240 frames at 48 kHz (`DefaultLookahead = 0.005`; exact frames scale with the sample rate). Total file length is unchanged; the tail simply ends 240 frames early.
- **Mono-bass peak.** `--mono-bass` sums correlated lows, which can nudge the pre-encode peak up (measured 0.998 → 1.209 on a hard-panned test mix). The WAV writer clamps out-of-range samples to full scale on encode, so output files never exceed 0 dBFS.
- **Reverb damping default.** `--reverb-damping=-1` (the default) keeps each preset's own damping; explicit `0.0`–`1.0` overrides it on every reverb instance.
- **Noise-gate units.** `--noise-gate` takes dBFS relative to full scale; `0` (the default) disables the gate, negative values mute sub-threshold voice output.
- **Score intake guard.** JSON scores are read through a 64 MiB limit reader (`maxScoreFileSize`) and automatically validated with `score.Validate` before parse-time processing.
- **Parallel determinism.** Parallel rendering (`--parallel`) voices tracks concurrently across a shared worker pool while the master sum stays sequential in track order. Output is mathematically byte-identical to sequential rendering (verified with SHA-256 and binary `cmp` comparisons). Track worker panics are caught with `recover()` and propagated through `errors.Join`, safely halting the render without process termination or buffer repeat glitches.

## Project Structure

| Directory | Description |
|-----------|-------------|
| `cmd/resonata` | CLI entry point, batch mode, pprof profiling helpers, signal handling |
| `pkg/dsp` | Oscillators, RBJ biquads, EQ, envelopes, FDN reverb, echo, limiter, buffers |
| `pkg/encoder` | Output format resolution and mono downmix |
| `pkg/engine` | Score scheduling, offline renderer, humanizer, phrase, and room handling |
| `pkg/instruments` | Instrument interface (`Instrument`, `PedalVoicer`) and per-note payload types |
| `pkg/instruments/ocarina` | Helmholtz physical-model reference voice |
| `pkg/instruments/sampler` | Polyphonic SFZ player: tokenizer, parser, path resolver, regions, voices |
| `pkg/instruments/synth` | Subtractive synth voice: dual oscillators, resonant lowpass, ADSR |
| `pkg/midi` | Standard MIDI File import and export |
| `pkg/mixer` | Track strips, per-track echo, multi-room reverb, soft clipper, worker pool |
| `pkg/score` | Score model, JSON parser, schema validator |
| `pkg/wav` | RIFF WAVE reader and writer |
| `docs` | User documentation, architecture guides, and LLM composition templates |
| `examples` | Example scores (JSON) |
| `test_data` | Test fixtures |
| `samples` | Sample-library fixtures and SFZ examples |
| `scripts` | Verification and maintenance scripts |

148 Go files. Further reading: [Architecture](./docs/ARCHITECTURE.md), [CLI reference](./docs/CLI_REFERENCE.md), [SFZ guide](./docs/SFZ_GUIDE.md), [cross-platform guide](./docs/CROSS_PLATFORM.md).

## Testing

```bash
go vet ./...
```

```bash
gofmt -l pkg/ cmd/
```

```bash
go test ./...
```

```bash
go test -race ./...
```

```bash
go test -cover ./...
```

```bash
bash scripts/verify_headless.sh
```

Current statement coverage (**99.3%** overall):
- `cmd/resonata`: 98.7%
- `pkg/dsp`: 99.6%
- `pkg/encoder`: 100.0%
- `pkg/engine`: 99.5%
- `pkg/instruments/ocarina`: 100.0%
- `pkg/instruments/sampler`: 99.3%
- `pkg/instruments/synth`: 99.3%
- `pkg/midi`: 100.0%
- `pkg/mixer`: 98.9%
- `pkg/score`: 100.0%
- `pkg/wav`: 100.0%

`gofmt -l` must print nothing; `verify_headless.sh` enforces the static headless build plus the Windows cross-compile.

## License

Licensed under AGPL-3.0. Any modifications to this code that are made available over a network must also be released under AGPL-3.0. See the LICENSE file for full terms.
For commercial licensing, proprietary use, or exceptions to AGPL requirements, contact jovan.license@gmail.com.
