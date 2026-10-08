# Resonata

Offline orchestral score renderer in pure Go: reads JSON/YAML scores or Standard MIDI Files and renders deterministic WAV audio with zero dependencies and zero CGO.

> [!TIP]
> If Resonata is useful to you, consider starring the repository — it helps others discover the project. Issues and contributions are welcome under the same license terms below.

## Overview

Resonata takes a declarative score (JSON or YAML), schedules every note to an exact audio frame, synthesizes each track through SFZ sample libraries, a subtractive synth voice, or a built-in physical-model voice, mixes through per-track EQ, echo, and convolution-free algorithmic reverb, and writes a mastered WAV file. The same input always produces byte-identical output: all randomness uses fixed seeds with deterministic scheduling, and the per-block render loop performs zero heap allocations.

> [!NOTE]
> Clone the repository and build from source as described in [Build](#build).

## Features

- JSON/YAML score DSL with validation that collects every violation into one error
- Standard MIDI File import (format 0/1) and export (format 1, 480 PPQ)
- Polyphonic SFZ sampler: cubic Hermite resampling, round-robin, random layers, crossfades, loop crossfade, voice stealing with fades
- Ocarina Helmholtz physical-model reference voice for pipeline smoke tests
- Subtractive synth voice: dual oscillators, resonant lowpass, ADSR, no sample files
- Per-track parametric EQ, independent stereo/ping-pong delay, per-room FDN reverb, tanh soft clipper, lookahead mastering limiter, LUFS metering
- Humanizer with Gaussian micro-timing and metric accents (deterministic seed)
- Sustain pedal (CC64) that holds sampler voices past note-off until release
- WAV output: 16/24/32-bit PCM and 32-bit float; 44100/48000/96000 Hz; mono/stereo
- Headless CLI with batch directory rendering, benchmarking, CPU/heap profiling, and optional parallel per-track rendering with identical output
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

Measured binary sizes (stripped, `CGO_ENABLED=0`): 3498144 bytes (~3.3 MB) Linux amd64, 3697152 bytes (~3.5 MB) Windows amd64.

## Quick Start

Print the version:

```bash
./bin/resonata --version
```

```
Resonata v2
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

The three cleanup flags combine in one render:

```bash
./bin/resonata --score=examples/simple_score.json --output=out.wav --saturation=clean --reverb-damping=0.0 --noise-gate=-60
```

`--saturation=clean` removes tanh coloration above the clipper knee, `--reverb-damping=0.0` keeps reverb tails bright, and `--noise-gate=-60` silences sample hiss below −60 dBFS. Each flag is independent; omit any of them to keep that stage at its default.

## CLI Reference

| Flag | Type | Default | Description |
|------|------|---------|-------------|
| `--score`, `-s`, `--load` | string | none | Input score file (JSON or YAML); `--load` is a legacy alias |
| `--output`, `-o`, `--out` | string | `out.wav` | Output WAV file path; `--out` is a legacy alias |
| `--mode` | string | `offline` | Render mode: `offline` or `stream` (both render identically) |
| `--sample-rate` | int | `48000` | Output sample rate in Hz: `44100`, `48000`, or `96000` |
| `--bit-depth` | string | `24` | Output encoding: `16`, `24`, `32` (integer PCM) or `32f` (32-bit IEEE float) |
| `--channels` | int | `2` | Output channels: `1` (mono, stereo downmixed) or `2` (stereo) |
| `--humanize` | float | `0` | Humanization strength 0.0–1.0 (deterministic, fixed seed) |
| `--reverb` | string | `hall` | Reverb preset: `cathedral`, `hall`, `chapel`, `room`, `plate`, or `none` (bypass) |
| `--master-gain` | float | `1.0` | Master gain multiplier 0.0–1.0 (out-of-range values clamp) |
| `--import-midi` | string | none | Render a Standard MIDI File instead of `--score` |
| `--export-midi` | string | none | Export the loaded score to a MIDI file after rendering |
| `--export-json` | string | none | With `--import-midi`, also write the imported score as JSON |
| `--profile-cpu` | string | none | Write a CPU profile to the path |
| `--profile-mem` | string | none | Write a heap profile to the path |
| `--verbose`, `-v` | bool | false | Verbose logging |
| `--benchmark` | bool | false | Render 10x in memory, report average, then write WAV |
| `--batch-dir` | string | none | Render every score and MIDI file in a directory |
| `--load-sfz` | string | none | Validate an SFZ library before rendering |
| `--version` | bool | false | Print `Resonata v2` and exit |
| `--saturation` | string | `tape` | `tape` (analog soft clip) or `clean` (linear clamp at ±1); unknown values error |
| `--reverb-damping` | float | `-1` (preset) | `-1` keeps preset damping; `0.0`–`1.0` overrides HF absorption (0 bright, 1 max cutoff) |
| `--noise-gate` | float | `0` (disabled) | `0` disables; negative dBFS mutes sub-threshold voice output |
| `--limiter` | bool | false | Master bus through the lookahead limiter (−0.1 dBFS ceiling) after the soft clipper |
| `--mono-bass` | string | `off` | `off` disables; integer Hz folds bass below cutoff to mono |
| `--parallel` | bool | false | Concurrent per-track voicing with sequential deterministic sum |

Exit codes: `0` on success, non-zero with a `resonata:`-prefixed message on parse, validation, I/O, or render failures. Progress percentages stream to stderr; only the final summary line goes to stdout.

## Score DSL Intro

Scores declare metadata and one track per part. Times are absolute seconds, pitches are MIDI numbers (60 = C4, 69 = A440):

```json
{
  "metadata": {"title": "Piece", "bpm": 120, "time_signature": "4/4"},
  "tracks": [
    {
      "id": "solo",
      "name": "Solo",
      "instrument": {"type": "ocarina"},
      "pan": 0.0,
      "volume": 0.8,
      "notes": [{"time": 0.0, "duration": 1.0, "pitch": 69, "velocity": 0.8}]
    }
  ]
}
```

The same score in YAML:

```yaml
metadata: {title: Piece, bpm: 120, time_signature: 4/4}
tracks:
  - id: solo
    name: Solo
    instrument: {type: ocarina}
    pan: 0.0
    volume: 0.8
    notes:
      - {time: 0.0, duration: 1.0, pitch: 69, velocity: 0.8}
```

Notes accept timing, envelope, vibrato, and expression overrides; tracks accept transposition, swing, breath pauses, EQ, delay, and per-track rooms. Full specification: [docs/SCORE_DSL.md](./docs/SCORE_DSL.md). LLM-ready composition templates live in [docs/composer_prompts/](./docs/composer_prompts/).

## Examples

Render a YAML score:

```bash
./bin/resonata --score=examples/simple_score.yaml --output=yaml.wav
```

Import a MIDI file and render it:

```bash
./bin/resonata --import-midi=test_data/mahler_symphony.mid --output=imported.wav
```

Export the loaded score back to MIDI after rendering:

```bash
./bin/resonata --score=examples/simple_score.json --output=out.wav --export-midi=out.mid
```

Render a whole directory (each input gets an isolated engine; WAVs land in the output directory):

```bash
mkdir -p renders/
./bin/resonata --batch-dir=./examples --output=renders/
```

Validate a sample library before rendering:

```bash
./bin/resonata --load-sfz=samples/piano.sfz --score=examples/test.json --output=out.wav
```

> [!WARNING]
> The bundled `samples/*.wav` files are placeholder tones for clean-checkout rendering, not production sound. Replace them with a real sample library for anything you intend to keep; see [docs/SAMPLE_LIBRARIES.md](./docs/SAMPLE_LIBRARIES.md).

## Output Formats

WAV combinations: 16-bit PCM, 24-bit PCM, 32-bit integer PCM, 32-bit IEEE float. `WAVE_FORMAT_EXTENSIBLE` is used for float and multi-channel layouts. Sample rates: 44100, 48000, 96000. Channel counts: mono (stereo downmixed), stereo.

```bash
./bin/resonata --score=examples/simple_score.json --output=out16.wav --bit-depth=16 --channels=1 --sample-rate=44100
./bin/resonata --score=examples/simple_score.json --output=outf.wav --bit-depth=32f --channels=2
```

## Performance

Measured on Intel Xeon E5-2640 v3, Go 1.27.1 (`-benchtime=200x`):

| Benchmark | Result | Allocations |
|-----------|--------|-------------|
| Oscillator, 1024 samples | 5610 ns/op | 0 B/op, 0 allocs/op |
| Biquad, 1024 samples | 6783 ns/op | 0 B/op, 0 allocs/op |
| Stereo delay, 1024 samples | 11593 ns/op | 0 B/op, 0 allocs/op |
| Mixer, 8 tracks | 235913 ns/op | 0 B/op, 0 allocs/op |
| Sampler note-on, random | 368.2 ns/op | 0 B/op, 0 allocs/op |
| Sampler note-on, sequence | 340.7 ns/op | 0 B/op, 0 allocs/op |

The audio loop never allocates heap memory: voices, envelopes, mixer buses, reverb instances, and echo ring lines are pre-allocated at construction, and every hot path carries an allocation test asserting zero. The CLI disables the garbage collector around the render loop so no GC pause can stall a long render. Absolute times vary with hardware; the zero-allocation guarantee does not.

## Technical Notes

- **Limiter lookahead.** With `--limiter`, the 5 ms lookahead buffer delays output by ~240 frames at 48 kHz (`DefaultLookahead = 0.005`; exact frames scale with the sample rate). Total file length is unchanged; the tail simply ends 240 frames early.
- **Mono-bass peak.** `--mono-bass` sums correlated lows, which can nudge the pre-encode peak up (measured 0.998 → 1.209 on a hard-panned test mix). The WAV writer clamps out-of-range samples to full scale on encode, so output files never exceed 0 dBFS.
- **Reverb damping default.** `--reverb-damping=-1` (the default) keeps each preset's own damping; explicit `0.0`–`1.0` overrides it on every reverb instance.
- **Noise-gate units.** `--noise-gate` takes dBFS relative to full scale; `0` (the default) disables the gate, negative values mute sub-threshold voice output.

Parallel rendering (`--parallel`) voices tracks concurrently across a shared worker pool while the master sum stays sequential in track order, so output is byte-identical to sequential rendering. Measured on the same machine above, a 50-track score renders in 15.44 s sequentially versus 4.80 s parallel (3.2×); gains scale with track count and core count.

## Project Structure

| Directory | Description |
|-----------|-------------|
| `cmd/resonata` | CLI entry point, batch mode, profiling helpers |
| `pkg/dsp` | Oscillators, biquads, EQ, envelopes, reverb, echo, limiter, buffers |
| `pkg/encoder` | Output format resolution and mono downmix |
| `pkg/engine` | Score scheduling, offline renderer, humanizer, phrase and room handling |
| `pkg/instruments` | Instrument interface and per-note payload types |
| `pkg/instruments/ocarina` | Helmholtz physical-model reference voice |
| `pkg/instruments/sampler` | Polyphonic SFZ player: parser, regions, voices |
| `pkg/instruments/synth` | Subtractive synth voice: dual oscillators, resonant lowpass, ADSR |
| `pkg/midi` | Standard MIDI File import and export |
| `pkg/mixer` | Track strips, per-track echo, multi-room reverb, soft clipper |
| `pkg/score` | Score model, JSON/YAML parsers, validator |
| `pkg/wav` | WAV reader and writer |
| `docs` | User documentation and LLM composition templates |
| `examples` | Example scores (JSON and YAML) |
| `test_data` | Test fixtures |
| `samples` | Sample-library fixtures and SFZ examples |
| `scripts` | Verification and maintenance scripts |

133 Go files. Further reading: [Architecture](./docs/ARCHITECTURE.md), [CLI reference](./docs/CLI_REFERENCE.md), [SFZ guide](./docs/SFZ_GUIDE.md), [cross-platform guide](./docs/CROSS_PLATFORM.md).

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
go test -cover ./...
```

```bash
bash scripts/verify_headless.sh
```

Current statement coverage: `cmd/resonata` 95.2%, `dsp` 96.7%, `encoder` 100.0%, `engine` 97.4%, `ocarina` 95.3%, `sampler` 98.7%, `synth` 95.1%, `midi` 95.3%, `mixer` 98.8%, `score` 96.0%, `wav` 95.7%. `gofmt -l` must print nothing; `verify_headless.sh` enforces the static headless build plus the Windows cross-compile.

## License

Licensed under AGPL-3.0. Any modifications to this code that are made available over a network must also be released under AGPL-3.0. See the LICENSE file for full terms.
For commercial licensing, proprietary use, or exceptions to AGPL requirements, contact jovan.license@gmail.com.
