# Resonata

Resonata is an offline orchestral score renderer written in pure Go with zero external dependencies and zero CGO. It reads JSON or YAML score files and renders them to WAV audio files.

## Version

Resonata v1.0. The version is pinned and will not change.

---

## Build

Linux:

```bash
CGO_ENABLED=0 go build -ldflags="-s -w" -o bin/resonata ./cmd/resonata
```

Windows:

```bash
CGO_ENABLED=0 go build -ldflags="-s -w" -o bin/resonata.exe ./cmd/resonata
```

`CGO_ENABLED` must be `0`. There are no build tags. The output path
`bin/resonata` is a convention; any path works. Requires Go 1.27 or
later.

## Run

Basic render:

```bash
./bin/resonata --score=examples/simple_score.json --output=out.wav
```

Render with humanize, reverb, and output format flags:

```bash
./bin/resonata --score=examples/epic_score.json --output=epic.wav --humanize=0.6 --reverb=cathedral --bit-depth=24 --channels=2 --sample-rate=48000
```

MIDI import:

```bash
./bin/resonata --import-midi=test_data/mahler_symphony.mid --output=imported.wav
```

Batch render:

```bash
./bin/resonata --batch-dir=./examples --reverb=hall
```

## CLI Flags

| Flag | Type | Default | Description |
|------|------|---------|-------------|
| `--score`, `-s`, `--load` | string | none | Input score file (JSON or YAML) |
| `--output`, `-o`, `--out` | string | `out.wav` | Output WAV file path |
| `--mode` | string | `offline` | Render mode: `offline` or `stream` |
| `--sample-rate` | int | `48000` | Output sample rate in Hz: `44100`, `48000`, or `96000` |
| `--bit-depth` | string | `24` | Output encoding: `16`, `24`, `32` (integer PCM) or `32f` (32-bit IEEE float) |
| `--channels` | int | `2` | Output channels: `1` (mono) or `2` (stereo) |
| `--humanize` | float | `0` | Humanization strength 0.0–1.0 |
| `--reverb` | string | `hall` | Reverb preset: `cathedral`, `hall`, `chapel`, `room`, `plate`, `none` |
| `--master-gain` | float | `1.0` | Master gain multiplier 0.0–1.0 |
| `--import-midi` | string | none | Render a Standard MIDI File instead of `--score` |
| `--export-midi` | string | none | Export the loaded score to a MIDI file after rendering |
| `--export-json` | string | none | With `--import-midi`, also write the imported score as JSON |
| `--profile-cpu` | string | none | Write a CPU profile to the path |
| `--profile-mem` | string | none | Write a heap profile to the path |
| `--verbose`, `-v` | bool | false | Verbose logging |
| `--benchmark` | bool | false | Render 10x in memory, report average, then write WAV |
| `--batch-dir` | string | none | Render every score and MIDI file in a directory |
| `--load-sfz` | string | none | Validate an SFZ library before rendering |
| `--version` | bool | false | Print `Resonata v1.0` and exit |

---

## Score Format

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

Full specification: [docs/SCORE_DSL.md](./docs/SCORE_DSL.md).

Supported note event fields: pitch, time, duration, velocity,
articulation, timing_offset_ms, accent, attack, decay, release,
vibrato, expression, phrase_id.

Supported track fields: id, name, instrument, pan, volume,
reverb_send, delay, eq, room, swing, breath, transpose, expression,
vibrato.

## Output Formats

WAV combinations: 16-bit PCM, 24-bit PCM, 32-bit integer PCM, 32-bit
IEEE float. `WAVE_FORMAT_EXTENSIBLE` is used for float and
multi-channel layouts. Sample rates: 44100, 48000, 96000. Channel
counts: mono, stereo.

---

## Project Structure

| Directory | Description | Lines |
|-----------|-------------|-------|
| `cmd/resonata` | CLI entry point, batch mode, profiling helpers | 623 |
| `pkg/dsp` | Oscillators, biquads, EQ, envelopes, reverb, echo, limiter, buffers | 3155 |
| `pkg/encoder` | Output format resolution and mono downmix | 173 |
| `pkg/engine` | Score scheduling, offline renderer, humanizer, phrase and room handling | 2045 |
| `pkg/instruments` | Instrument interface and per-note payload types | 65 |
| `pkg/instruments/ocarina` | Helmholtz physical model reference voice | 649 |
| `pkg/instruments/sampler` | Polyphonic SFZ player: parser, regions, voices | 5688 |
| `pkg/midi` | Standard MIDI File import and export | 1688 |
| `pkg/mixer` | Track strips, per-track echo, multi-room reverb, soft clipper | 1029 |
| `pkg/score` | Score model, JSON/YAML parsers, validator | 2217 |
| `pkg/wav` | WAV reader and writer | 1619 |
| `docs` | User documentation | — |
| `docs/composer_prompts` | LLM score-generation templates | — |
| `examples` | Example scores | — |
| `test_data` | Test fixtures | — |
| `samples` | Sample libraries and SFZ fixtures | — |
| `scripts` | Verification and maintenance scripts | — |
| `bin` | Built binaries (not versioned) | — |

Line counts cover `*.go` in `cmd` and `pkg` only.

## Metrics

| Metric | Value |
|--------|-------|
| Total Go files | 88 |
| Total lines of Go code | 18951 |
| Test coverage percentage | 89.7 |
| Binary size for Linux amd64 in megabytes | 3.29 |
| Binary size for Windows amd64 in megabytes | 3.49 |
| Go version used for the build | 1.27.1 |
| Build machine CPU model | Intel Xeon E5-2640 v3 |
| Build machine core count | 8 cores, 16 threads |
| Build machine RAM | 32 GB |

Coverage is the mean across the seven core packages (dsp 93.5,
mixer 96.6, wav 88.8, score 89.8, sampler 84.6, engine 80.4, midi
94.1). Binary sizes are for `CGO_ENABLED=0` builds with stripped
symbols (`-ldflags="-s -w"`): 3453088 bytes (Linux) and 3654656
bytes (Windows).

Binary size may vary by approximately 5 to 15 percent depending on
the Go version used for compilation, the target operating system,
and whether debug symbols are stripped. The binary is fully static
with no shared library dependencies. Binary size is independent of
the target machine's CPU, RAM, or disk speed.

## Performance

| Benchmark | Result | Allocations |
|-----------|--------|-------------|
| 50-track one-minute full render | 15.96 s per render | 0 steady-state (one-time setup only) |
| Oscillator, 1024 samples | 9591 ns/op | 0 B/op, 0 allocs/op |
| Biquad, 1024 samples | 10235 ns/op | 0 B/op, 0 allocs/op |
| Mixer, 8 tracks | 393913 ns/op | 0 B/op, 0 allocs/op |
| Sampler note-on, sequence | 531 ns/op | 0 B/op, 0 allocs/op |
| Sampler note-on, random | 338 ns/op | 0 B/op, 0 allocs/op |

These numbers are from the build machine listed in the metrics
section and will vary on different hardware. The full render
allocates once during engine construction; the per-block audio loop
allocates nothing.

---

## Features

- SFZ sampler with cubic Hermite resampling
- Bulletproof SFZ parser with include support
- Round-robin with seq_length and seq_position
- Random selection with lorand and hirand
- note_polyphony enforcement
- Same-pitch auto-fade
- xfin/xfout crossfade layers
- Loop crossfade
- Voice steal fade
- Ocarina Helmholtz physical model
- Per-track EQ with high-pass filter and three-band parametric
- Per-track independent delay with ping-pong and BPM sync
- Algorithmic reverb with FDN topology
- Stereo panning
- Tanh soft clipper
- Mastering limiter with lookahead
- LUFS loudness metering
- Humanizer with Gaussian timing and metric accents
- MIDI import and export for Format 0 and Format 1
- Score transposition global and per-track
- Note-level timing offset and swing
- Per-note envelope override
- Vibrato and expression
- Phrase grouping and breath pauses
- Per-track room with reverb presets
- WAVE_FORMAT_EXTENSIBLE output
- Headless CLI with batch rendering

## Known Limitations

- Offline rendering only, no real-time playback.
- No GUI.
- Ocarina is a reference voice for pipeline validation, not production quality.
- Single-goroutine audio path by design.
- Voice limit defaults to 16.
- Loop crossfade is skipped for loops shorter than 8 milliseconds.
- Binary size depends on Go version.
- No MP3 or OGG output.

## Notes

- The project is written in pure Go with zero external dependencies and zero CGO.
- All audio processing is zero-allocation with pre-allocated buffers.
- The garbage collector is disabled during render and re-enabled after.
- The ocarina is a procedural reference voice for smoke-testing the pipeline without external sample libraries.
- For production orchestral rendering, use the SFZ sampler with real sample libraries.
- The SFZ parser is designed to load real-world libraries without manual cleanup, including handling of Windows backslashes, case-insensitive paths on Linux, unknown opcodes, and recursive include directives with cycle detection.
- The score DSL is designed to be generated by large language models.
- The composer prompt files in docs/composer_prompts provide ready-made templates for LLM score generation.
- The project does not use Git in its development workflow. Storage and version control are handled externally by the developer.
- The project is not intended for public distribution. It is an internal tool for the developer and for companies that receive it.
- The repository contains only source code, documentation, example scores, and test fixtures. Binaries, render outputs, profiling data, and coverage files are generated by `go build` and `go test` and are excluded by `.gitignore`; do not commit or preserve them.

---

## License

Licensed under AGPL-3.0. Any modifications to this code that are made available over a network must also be released under AGPL-3.0. See the LICENSE file for full terms.
For commercial licensing, proprietary use, or exceptions to AGPL requirements, contact jovan.license@gmail.com.
