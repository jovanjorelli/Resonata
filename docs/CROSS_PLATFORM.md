# Cross-Platform Guide

Resonata runs identically on Linux and Windows from pure Go with
`CGO_ENABLED=0`. This document covers builds, paths, and
platform-specific troubleshooting.

## Binary compatibility

Static builds carry no GUI or system audio dependencies:

```bash
# Linux binary
CGO_ENABLED=0 go build -ldflags="-s -w" -o bin/resonata ./cmd/resonata

# Native Windows binary
CGO_ENABLED=0 go build -ldflags="-s -w" -o bin/resonata.exe ./cmd/resonata

# Cross-compile Linux -> Windows
CGO_ENABLED=0 GOOS=windows GOARCH=amd64 go build -ldflags="-s -w" -o bin/resonata.exe ./cmd/resonata
```

The Linux binary has zero dependencies on X11, Wayland, or GPU
libraries: it is fully static. `scripts/verify_headless.sh` enforces
the 15 MB stripped size limit and the Windows cross-compile.

## Path handling

All sample and include paths pass through `SanitizePath`
(`pkg/instruments/sampler/path_resolver.go`):

| Input | Normalized |
|-------|------------|
| `samples\piano\c4.wav` | `samples/piano/c4.wav` |
| `"Grand Piano/c4 loud.wav"` | `Grand Piano/c4 loud.wav` |
| `samples//piano//c4.wav` | `samples/piano/c4.wav` |

Backslashes become forward slashes, surrounding quotes are stripped,
and repeated separators collapse. Score-relative SFZ references
resolve against the working directory; sample references resolve
against the SFZ file's directory.

### Case-insensitive path resolution (Linux and macOS)

Linux filesystems are typically case-sensitive, but many orchestral
libraries are authored on Windows with inconsistent casing. When the
exact path misses, Resonata walks each directory level comparing
lowercase names. Results cache per lowercase key across nested
`#include` files. On Windows the filesystem itself is
case-insensitive, so the direct check wins. Smoke-test any library
with:

```bash
./bin/resonata --load-sfz=samples/windows_authored_library/main.sfz --score=examples/test.json --output=out.wav --verbose
```

### Sample library best practices

Use forward slashes in SFZ files even on Windows. Avoid
case-variant duplicates (`piano.wav` and `Piano.wav`) in one
directory. Quote paths with spaces.

## Filesystem layout (recommended)

```text
resonata/
├── bin/resonata          # binary
├── samples/
│   ├── windows_authored_library/  # bundled cross-platform fixture
│   └── custom/              # authored SFZ files
├── examples/
│   └── my_symphony.json
└── renders/
    └── my_symphony.wav
```

Keep large third-party libraries out of version control; only small
fixtures belong in `samples/`.

## Line endings

SFZ, JSON, and YAML inputs work with LF and CRLF endings. The SFZ
include expander reads lines with a 1 MB buffer; the score parser
selects YAML or JSON by file extension.

## Performance

Render speed scales with polyphony and effect tails. A 40 s dense
score renders in about 5 s on a modern desktop (~0.1x realtime).
Measure with `--benchmark` (10 in-memory renders, average reported)
and profile with `--profile-cpu` plus `go tool pprof`.

## Troubleshooting

Include cycles (`a.sfz` -> `b.sfz` -> `a.sfz`) abort naming the
repeated file; diamond includes expand once per branch. Samples found
on Windows but not Linux indicate a casing mismatch: compare `ls`
output against the `--verbose` sanitized path. Slow renders yield to
process priority (`nice` on Linux, priority class on Windows); very
large libraries can exceed RAM because samples decode fully on first
use, so prefer one SFZ per instrument.

## Building from source

Prerequisites: Go 1.27 or later, no system libraries.

```bash
git clone https://github.com/yourname/resonata.git
cd resonata
CGO_ENABLED=0 go build -ldflags="-s -w" -o bin/resonata ./cmd/resonata
./bin/resonata --help
```

Tests and linters:

```bash
go vet ./...
go test ./...
gofmt -l pkg/ cmd/
```

## Containerization

Minimal-container friendly (no runtime dependencies):

```dockerfile
FROM golang:1.27-alpine AS build
WORKDIR /app
COPY . .
RUN CGO_ENABLED=0 go build -ldflags="-s -w" -o /resonata ./cmd/resonata

FROM alpine:3.19
COPY --from=build /resonata /usr/local/bin/
ENTRYPOINT ["/usr/local/bin/resonata"]
```

```bash
docker build -t resonata .
docker run -v $(pwd):/work -w /work resonata --score=examples/simple_score.json -o out.wav
```
