# Sample Libraries

The WAV files under `samples/` are procedural placeholder tones
(sine waves, peak ~0.3), not recorded instruments. They exist so every
shipped example renders on a clean checkout: real orchestral libraries
are hundreds of megabytes and carry third-party licenses, so neither
belongs in this repository.

## Library layout

Drop a library under `samples/<name>/` with its SFZ next to its WAVs,
matching how `samples/piano.sfz` sits beside `samples/c4.wav` and
`samples/c5.wav`. Point the score's `instrument.file` at the SFZ path,
as `examples/epic_score.json` points at `samples/piano.sfz`.

On Linux, sample lookups try the exact path first, then fall back to a
case-insensitive directory walk (`pkg/instruments/sampler/path_resolver.go:71`,
walk at `pkg/instruments/sampler/path_resolver.go:101`; skipped on
Windows at `pkg/instruments/sampler/path_resolver.go:90`). Prefer exact
forward-slash paths anyway: the fallback repairs Windows-authored
casing, it does not bless new mismatches. Separators and quotes are
normalized by `SanitizePath`
(`pkg/instruments/sampler/path_resolver.go:53`).

`#include` expansion recurses to a depth cap of 64 with cycle detection
(`pkg/instruments/sampler/include.go:13`); cycles abort naming the
repeated file. Samples decode fully to RAM on first use, so prefer one
SFZ per instrument (`docs/CROSS_PLATFORM.md:101-102`).

## Swap procedure

1. Delete the placeholder WAVs listed in `samples/README.md`
   (`samples/c4.wav`, `samples/c5.wav`, and the three under
   `samples/windows_authored_library/`).
2. Copy the licensed library into `samples/` preserving its internal
   relative layout.
3. Render the two sampler examples and expect exit 0:
   `./bin/resonata --score=examples/epic_score.json --output=epic.wav`
   `./bin/resonata --score=examples/test.json --output=smoke.wav`
4. On `resolve ...: file does not exist` errors, fix the on-disk path
   case first, then the SFZ `sample=` references.

## Licensing warning

AGPL-3.0 covers the render engine only. Sample libraries keep their
own licenses; most prohibit redistribution. Do not commit licensed WAVs
to this repository (`samples/**/*.wav` stays ignored except the
bundled placeholders).

## Verification

```bash
CGO_ENABLED=0 go build -ldflags="-s -w" -o bin/resonata ./cmd/resonata
./bin/resonata --score=examples/epic_score.json --output=/tmp/epic.wav
./bin/resonata --score=examples/test.json --output=/tmp/smoke.wav
sha256sum /tmp/epic.wav && ./bin/resonata --score=examples/epic_score.json --output=/tmp/epic2.wav && sha256sum /tmp/epic2.wav
```

The two hashes for the repeated render must be identical
(deterministic seeds); both renders must exit 0 with valid RIFF/WAVE
headers.
