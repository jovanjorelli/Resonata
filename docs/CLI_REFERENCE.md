# CLI Reference — Resonata v1.0

Complete reference for the `resonata` headless renderer. For engine
internals, see [Architecture](./ARCHITECTURE.md); for scores, see
[Score DSL](./SCORE_DSL.md).

## Synopsis

```bash
resonata [flags]
```

Build first:

```bash
CGO_ENABLED=0 go build -ldflags="-s -w" -o bin/resonata ./cmd/resonata
```

## Core flags

| Flag | Short | Type | Default | Description |
|------|-------|------|---------|-------------|
| `--score` | `-s` | string | none | Input score file (JSON or YAML); `--load` is a legacy alias |
| `--output` | `-o` | string | `out.wav` | Output WAV file path; `--out` is a legacy alias |
| `--mode` | none | string | `offline` | Render mode: `offline` or `stream` |
| `--sample-rate` | none | int | `48000` | Output sample rate in Hz: `44100`, `48000`, or `96000` |
| `--bit-depth` | none | string | `24` | Output encoding: `16`, `24`, `32` (integer PCM) or `32f` (32-bit IEEE float) |
| `--channels` | none | int | `2` | Output channels: `1` (mono, stereo downmixed) or `2` (stereo) |
| `--verbose` | `-v` | bool | false | Verbose logging |
| `--version` | none | bool | false | Print `Resonata v1.0` and exit |

## MIDI interoperability

| Flag | Description |
|------|-------------|
| `--import-midi=FILE` | Import a Standard MIDI File (format 0 or 1) and render it directly instead of `--score` |
| `--export-midi=FILE` | Export the loaded score to a format-1 Standard MIDI File (480 PPQ) after rendering |
| `--export-json=FILE` | With `--import-midi`: also write the imported score as JSON |

`--import-midi` replaces `--score` as the render source.

## Audio processing

| Flag | Range | Default | Description |
|------|-------|---------|-------------|
| `--humanize` | 0.0-1.0 | 0.0 | Micro-timing and velocity humanization strength, deterministic seed 1 |
| `--reverb` | preset | `hall` | Reverb space: `cathedral`, `hall`, `chapel`, `room`, `plate`, or `none` (bypass) |
| `--master-gain` | 0.0-1.0 | 1.0 | Master output gain multiplier applied before WAV encoding |

## SFZ library flags

| Flag | Description |
|------|-------------|
| `--load-sfz=FILE` | Validate an SFZ library (regions, samples, includes) before rendering; the score render proceeds afterwards |

## Diagnostics

| Flag | Description |
|------|-------------|
| `--profile-cpu=FILE` | Write a CPU profile (pprof format) covering the render |
| `--profile-mem=FILE` | Write a heap profile after the render |
| `--benchmark` | Render 10x in memory without file I/O, report the average time, then write the WAV |

## Batch processing

| Flag | Description |
|------|-------------|
| `--batch-dir=DIR` | Render every `.json`, `.yaml`, `.yml`, `.mid`, and `.midi` file in the directory; each file gets an isolated engine |

With `--output` naming a directory, WAV files land there under the
input base name; otherwise each WAV lands next to its input file.

## Examples

Basic render:

```bash
./bin/resonata -s score.json -o output.wav
```

Humanized render with reverb:

```bash
./bin/resonata \
  --score=examples/epic_score.json \
  --output=symphony_hq.wav \
  --humanize=0.6 \
  --reverb=cathedral \
  --master-gain=0.95
```

Output format matrix:

```bash
./bin/resonata -s score.json -o out16.wav --bit-depth=16 --channels=1 --sample-rate=44100
./bin/resonata -s score.json -o out24.wav --bit-depth=24
./bin/resonata -s score.json -o out32.wav --bit-depth=32 --sample-rate=96000
./bin/resonata -s score.json -o outf.wav --bit-depth=32f --channels=2
```

Integer PCM writes the standard 16-byte `fmt` chunk; 32-bit float and
multi-channel layouts use `WAVE_FORMAT_EXTENSIBLE` with channel mask
and SubFormat GUID.

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
| 1 | General error (parse, validation, I/O, SFZ load, render) |

Failures report through `log.Fatal` with a `resonata:` prefix.
Progress percentages stream to stderr; only the final summary line
goes to stdout.
