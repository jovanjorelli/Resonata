# CLI Reference — Resonata v3

Complete reference for the `resonata` headless renderer. For engine
internals, see [Architecture](./ARCHITECTURE.md); for scores, see
[Score DSL](./SCORE_DSL.md).

## Synopsis

```bash
resonata [flags]
```

Build from source:

```bash
CGO_ENABLED=0 go build -ldflags="-s -w" -o bin/resonata ./cmd/resonata
```

## Core flags

| Flag | Short | Type | Default | Description |
|------|-------|------|---------|-------------|
| `--score` | `-s` | string | none | JSON score file (maximum 64 MiB, auto-validated on load); `--load` is a legacy alias |
| `--output` | `-o` | string | `out.wav` | Output WAV file path; `--out` is a legacy alias |
| `--mode` | none | string | `offline` | Render mode: `offline` or `stream` (both render to file; `stream` is reserved for future DAC streaming) |
| `--sample-rate` | none | int | `48000` | Output sample rate in Hz: `44100`, `48000`, or `96000` |
| `--bit-depth` | none | string | `24` | Output encoding: `16`, `24`, `32` (integer PCM) or `32f` (32-bit IEEE float) |
| `--channels` | none | int | `2` | Output channels: `1` (mono, stereo downmixed) or `2` (stereo) |
| `--verbose` | `-v` | bool | false | Verbose diagnostic logging |
| `--version` | none | bool | false | Print `Resonata v3` and exit |

## MIDI interoperability

| Flag | Description |
|------|-------------|
| `--import-midi=FILE` | Import a Standard MIDI File (format 0 or 1) and render it directly instead of `--score` |
| `--export-midi=FILE` | Export the loaded score to a format-1 Standard MIDI File (480 PPQ) after rendering |
| `--export-json=FILE` | With `--import-midi`: also write the imported score as JSON |

`--import-midi` replaces `--score` as the render source. Note-on events with velocity 0 are automatically mapped to note-offs, and running status is fully handled.

## Audio processing

| Flag | Range | Default | Description |
|------|-------|---------|-------------|
| `--humanize` | 0.0-1.0 | 0.0 | Micro-timing and velocity humanization strength, deterministic seed 1 |
| `--reverb` | preset | `hall` | Reverb space: `cathedral`, `hall`, `chapel`, `room`, `plate`, or `none` (bypass) |
| `--master-gain` | 0.0-1.0 | 1.0 | Master output gain multiplier applied before WAV encoding |
| `--saturation` | `tape`, `clean` | `tape` | Analog soft clip (`tape`) or linear clamp at ±1.0 (`clean`); unknown values error |
| `--reverb-damping` | 0.0-1.0 | -1 (preset) | `-1` keeps preset damping; `0.0`–`1.0` overrides HF absorption (0 bright, 1 max cutoff); else errors |
| `--noise-gate` | dBFS | 0 (off) | `0` disables; negative values mute sub-threshold voice output; positive/NaN rejected |
| `--limiter` | bool | false | Master bus through the lookahead limiter (−0.1 dBFS ceiling) after the soft clipper |
| `--mono-bass` | `off` or Hz | `off` | `off` disables; integer Hz folds bass below cutoff to mono; else errors |
| `--parallel` | bool | false | Concurrent per-track voicing with sequential deterministic sum (byte-identical output) |

## SFZ library flags

| Flag | Description |
|------|-------------|
| `--load-sfz=FILE` | Validate an SFZ library (regions, samples, recursive includes, symlinks) before rendering; score render proceeds afterwards |

## Diagnostics

| Flag | Description |
|------|-------------|
| `--profile-cpu=FILE` | Write a CPU profile (pprof format) covering the render (thread-safe, mutex-synchronized) |
| `--profile-mem=FILE` | Write a heap profile after the render |
| `--benchmark` | Render 10x in memory without file I/O, report average wall time, then write the WAV |

## Batch processing

| Flag | Description |
|------|-------------|
| `--batch-dir=DIR` | Render every `.json`, `.mid`, and `.midi` file in the directory; each file gets an isolated engine |

In batch mode:
- If `--output` names an existing directory, WAV files land there under the input file's base name.
- Otherwise, each WAV file is generated alongside its input score.
- Panics during individual file renders are isolated with `recover()`, reporting a stack trace and continuing batch queue processing without crashing the CLI.

## Examples

Basic render:

```bash
./bin/resonata -s examples/simple_score.json -o output.wav
```

Humanized render with cathedral reverb:

```bash
./bin/resonata \
  --score=examples/epic_score.json \
  --output=symphony_hq.wav \
  --humanize=0.6 \
  --reverb=cathedral \
  --master-gain=0.95
```

Parallel multi-track render:

```bash
./bin/resonata \
  --score=examples/epic_score.json \
  --output=parallel_render.wav \
  --parallel \
  --limiter
```

Output format matrix:

```bash
./bin/resonata -s examples/simple_score.json -o out16.wav --bit-depth=16 --channels=1 --sample-rate=44100
./bin/resonata -s examples/simple_score.json -o out24.wav --bit-depth=24
./bin/resonata -s examples/simple_score.json -o out32.wav --bit-depth=32 --sample-rate=96000
./bin/resonata -s examples/simple_score.json -o outf.wav --bit-depth=32f --channels=2
```

Integer PCM writes the standard 16-byte `fmt` chunk; 32-bit float, 24-bit in 32-bit container formats, and multi-channel layouts use `WAVE_FORMAT_EXTENSIBLE` with the channel mask and SubFormat GUID.

MIDI round-trip:

```bash
./bin/resonata \
  --import-midi=test_data/mahler_symphony.mid \
  --export-midi=final.mid \
  --output=final.wav
```

Batch render:

```bash
./bin/resonata --batch-dir=./examples --reverb=hall
```

Profile a render:

```bash
./bin/resonata -s examples/epic_score.json -o out.wav --profile-cpu=cpu.prof
go tool pprof -top cpu.prof
```

## Exit codes

| Code | Meaning |
|------|---------|
| 0 | Success |
| 1 | General error (parse, validation, I/O, SFZ load, worker panic, audio failure) |

Failures report through `log.Fatal` with a `resonata:` prefix.
Progress percentages stream to stderr; only the final summary line
goes to stdout.
