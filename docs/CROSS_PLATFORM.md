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

The Linux binary has zero dependencies on X11, Wayland, ALSA, PulseAudio,
PipeWire, or GPU drivers: it is fully static. `scripts/verify_headless.sh`
enforces the stripped size limit (~3.5 MB) and the Windows cross-compile.

## Path handling

All sample and include paths pass through `SanitizePath`
(`pkg/instruments/sampler/path_resolver.go`):

| Input | Normalized |
|-------|------------|
| `samples\piano\c4.wav` | `samples/piano/c4.wav` |
| `"Grand Piano/c4 loud.wav"` | `Grand Piano/c4 loud.wav` |
| `samples//piano//c4.wav` | `samples/piano/c4.wav` |
| `C:\Samples\kick.wav` (on Linux) | `Samples/kick.wav` |

- Backslashes become forward slashes.
- Surrounding quotes are stripped.
- Repeated slashes collapse.
- Windows drive letters (`C:`, `D:`) are stripped when resolving under Unix systems.
- Relative paths are anchored against the instrument library's root directory.

### Symlink path traversal protection

When resolving paths across symbolic links, `isWithinBase` verifies that
the real canonical path (`filepath.EvalSymlinks`) remains strictly inside
the library root directory (`realBase`). Symlink escapes pointing to parent
or system directories (e.g. `/etc` or `/var`) are caught and rejected
with `os.ErrNotExist`.

### Case-insensitive path resolution (Linux and macOS)

Linux filesystems are typically case-sensitive, but many orchestral
libraries are authored on Windows with inconsistent casing. When the
exact path misses, Resonata walks each directory level comparing
lowercase names. Results are cached in `resolveCache` with non-blocking
`atomic.Uint64` hit and miss counters, ensuring zero lock contention
during `--parallel` multi-track renders.

Smoke-test an SFZ library before rendering:

```bash
./bin/resonata --load-sfz=samples/piano.sfz --score=examples/simple_score.json -o out.wav
```

## Batch processing across operating systems

The batch renderer (`--batch-dir=DIR`) traverses score directories
using standard Go filepath abstractions. Path separators and forward/backslash
differences are normalized transparently across platforms. Panics on
individual malformed files are caught with `recover()` so batch queue
execution never halts prematurely.
