package sampler

import (
	"bufio"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// maxIncludeDepth bounds recursive #include expansion against hostile
// files. Cycles are reported by name; the depth cap is a backstop.
const maxIncludeDepth = 64

// IncludeExpander expands #include directives recursively with cycle
// detection. Paths resolve relative to the including file through a
// shared case-insensitive cache.
type IncludeExpander struct {
	resolver *PathResolver
	seen     map[string]bool // files on the current expansion stack
	depth    int
}

// NewIncludeExpander returns an expander sharing resolver's cache.
func NewIncludeExpander(resolver *PathResolver) *IncludeExpander {
	return &IncludeExpander{
		resolver: resolver,
		seen:     make(map[string]bool),
	}
}

// Expand reads an SFZ file and recursively expands #include directives,
// returning the fully expanded content. Include cycles report an error
// naming the repeated file.
func (ie *IncludeExpander) Expand(sfzPath string) ([]byte, error) {
	absPath, err := filepath.Abs(sfzPath)
	if err != nil {
		return nil, err
	}
	absPath = filepath.Clean(absPath)
	if ie.seen[absPath] {
		return nil, fmt.Errorf("include cycle detected: %s", absPath)
	}
	if ie.depth >= maxIncludeDepth {
		return nil, fmt.Errorf("include depth exceeded at %s", absPath)
	}
	ie.seen[absPath] = true
	ie.depth++
	defer func() {
		delete(ie.seen, absPath)
		ie.depth--
	}()

	f, err := os.Open(absPath)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	var out strings.Builder
	scanner := bufio.NewScanner(f)
	scanner.Buffer(make([]byte, 64*1024), 1024*1024)

	dir := filepath.Dir(absPath)
	lineNum := 0
	for scanner.Scan() {
		lineNum++
		line := strings.TrimSpace(scanner.Text())
		if isIncludeLine(line) {
			incPath := parseIncludePath(line)
			if incPath == "" {
				return nil, fmt.Errorf("%s line %d: empty #include path", absPath, lineNum)
			}
			resolved, err := ie.resolver.ForDir(dir).Resolve(incPath)
			if err != nil {
				return nil, fmt.Errorf("%s line %d: include not found: %w", absPath, lineNum, err)
			}
			included, err := ie.Expand(resolved)
			if err != nil {
				return nil, fmt.Errorf("%s line %d: %w", absPath, lineNum, err)
			}
			out.Write(included)
			out.WriteByte('\n')
			continue
		}
		out.WriteString(line)
		out.WriteByte('\n')
	}
	if err := scanner.Err(); err != nil {
		return nil, err
	}
	return []byte(out.String()), nil
}

// isIncludeLine reports whether line is an #include directive. The match
// is case-insensitive and ignores leading whitespace (already trimmed by
// the caller contract); commented lines starting with // or ; never
// match.
func isIncludeLine(line string) bool {
	if len(line) < len("#include") {
		return false
	}
	if !strings.EqualFold(line[:len("#include")], "#include") {
		return false
	}
	rest := line[len("#include"):]
	return rest == "" || rest[0] == ' ' || rest[0] == '\t' || rest[0] == '"' || rest[0] == '\''
}

// parseIncludePath extracts the path from an #include line, handling
// quoted and unquoted forms with trailing comments.
func parseIncludePath(line string) string {
	rest := strings.TrimSpace(line[len("#include"):])
	if rest == "" {
		return ""
	}
	if rest[0] == '"' || rest[0] == '\'' {
		quote := rest[0]
		if end := strings.IndexByte(rest[1:], quote); end >= 0 {
			return strings.TrimSpace(rest[1 : end+1])
		}
		return ""
	}
	fields := strings.Fields(rest)
	if len(fields) == 0 {
		return ""
	}
	return fields[0]
}
