package sampler

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
)

// resolveCache is a shared lookup table from lowercase path to the
// on-disk path. Resolvers created for nested #include directories share
// one cache so repeated loads stay fast. It is safe for concurrent use.
type resolveCache struct {
	mu      sync.RWMutex
	entries map[string]string
	hits    uint64
	misses  uint64
}

// PathResolver handles cross-platform path resolution with a
// case-insensitive fallback on Linux and macOS. Windows paths are
// natively case-insensitive, so the fallback is skipped there.
type PathResolver struct {
	basePath  string
	cache     *resolveCache
	isWindows bool
}

// NewPathResolver returns a resolver rooted at basePath. Sample paths in
// SFZ files resolve relative to this directory.
func NewPathResolver(basePath string) *PathResolver {
	return &PathResolver{
		basePath:  filepath.Clean(basePath),
		cache:     &resolveCache{entries: make(map[string]string)},
		isWindows: runtime.GOOS == "windows",
	}
}

// ForDir returns a resolver for a subdirectory that shares the lookup
// cache with r. Nested #include files use this so directory scans are
// never repeated.
func (r *PathResolver) ForDir(dir string) *PathResolver {
	return &PathResolver{
		basePath:  filepath.Clean(dir),
		cache:     r.cache,
		isWindows: r.isWindows,
	}
}

// SanitizePath normalizes separators and strips surrounding quotes.
// Input `"Grand Piano\c4 loud.wav"` becomes `Grand Piano/c4 loud.wav`.
func SanitizePath(p string) string {
	p = strings.TrimSpace(p)
	if len(p) >= 2 {
		if (p[0] == '"' && p[len(p)-1] == '"') ||
			(p[0] == '\'' && p[len(p)-1] == '\'') {
			p = p[1 : len(p)-1]
		}
	}
	p = strings.ReplaceAll(p, "\\", "/")
	for strings.Contains(p, "//") {
		p = strings.ReplaceAll(p, "//", "/")
	}
	return strings.TrimSpace(p)
}

// Resolve finds the on-disk path for relativePath. It first tries the
// direct path, then falls back to a case-insensitive directory walk on
// Linux and macOS. Absolute paths bypass the base directory.
func (r *PathResolver) Resolve(relativePath string) (string, error) {
	sanitized := SanitizePath(relativePath)
	if sanitized == "" {
		return "", &os.PathError{Op: "resolve", Path: relativePath, Err: os.ErrNotExist}
	}
	if filepath.IsAbs(filepath.FromSlash(sanitized)) {
		abs := filepath.FromSlash(sanitized)
		if _, err := os.Stat(abs); err == nil {
			return abs, nil
		}
		return "", &os.PathError{Op: "resolve", Path: relativePath, Err: os.ErrNotExist}
	}
	absPath := filepath.Join(r.basePath, filepath.FromSlash(sanitized))

	// Fast path: exact match.
	if _, err := os.Stat(absPath); err == nil {
		return absPath, nil
	}

	// Windows filesystems are already case-insensitive.
	if r.isWindows {
		return "", &os.PathError{Op: "resolve", Path: relativePath, Err: os.ErrNotExist}
	}

	// Slow path: case-insensitive walk with result caching.
	return r.resolveCaseInsensitive(sanitized)
}

// resolveCaseInsensitive walks the directory tree comparing lowercase
// names, caching successful resolutions.
func (r *PathResolver) resolveCaseInsensitive(path string) (string, error) {
	key := strings.ToLower(path)
	r.cache.mu.RLock()
	if cached, ok := r.cache.entries[key]; ok {
		r.cache.mu.RUnlock()
		r.cache.mu.Lock()
		r.cache.hits++
		r.cache.mu.Unlock()
		return cached, nil
	}
	r.cache.mu.RUnlock()

	parts := strings.Split(path, "/")
	current := r.basePath
	for _, part := range parts {
		if part == "" || part == "." {
			continue
		}
		if part == ".." {
			current = filepath.Dir(current)
			continue
		}
		entries, err := os.ReadDir(current)
		if err != nil {
			return "", err
		}
		target := strings.ToLower(part)
		found := false
		for _, entry := range entries {
			if strings.ToLower(entry.Name()) == target {
				current = filepath.Join(current, entry.Name())
				found = true
				break
			}
		}
		if !found {
			return "", &os.PathError{Op: "resolve", Path: path, Err: os.ErrNotExist}
		}
	}

	r.cache.mu.Lock()
	r.cache.entries[key] = current
	r.cache.misses++
	r.cache.mu.Unlock()
	return current, nil
}

// Hits reports cache hits on the slow path.
func (r *PathResolver) Hits() uint64 {
	r.cache.mu.RLock()
	defer r.cache.mu.RUnlock()
	return r.cache.hits
}

// Misses reports slow-path resolutions performed.
func (r *PathResolver) Misses() uint64 {
	r.cache.mu.RLock()
	defer r.cache.mu.RUnlock()
	return r.cache.misses
}
