package sampler

import (
	"os"
	"path/filepath"
	"testing"
)

// FuzzSFZParse feeds arbitrary bytes to the tolerant SFZ text parser.
// ParseSFZ must never panic and always returns a nil error; malformed
// values are dropped and counted, unknown opcodes ignored.
func FuzzSFZParse(f *testing.F) {
	seeds := []string{
		"../../..//test_data/messy_real_world.sfz",
		"../../../samples/piano.sfz",
		"../../../samples/windows_authored_library/main.sfz",
		"../../../samples/windows_authored_library/Strings/extra.sfz",
	}
	for _, s := range seeds {
		data, err := os.ReadFile(filepath.Clean(s))
		if err != nil {
			continue // seed missing on slim checkouts; corpus still valid
		}
		f.Add(string(data))
	}
	f.Add("<region> sample=x.wav lokey=60 hikey=64 pitch_keycenter=60")
	f.Add("<control> default_path=Samples\\\n<region> sample=\"a\\b.wav\" lokey=0 hikey=127")

	f.Fuzz(func(t *testing.T, text string) {
		got, err := ParseSFZ([]byte(text))
		if err != nil {
			t.Fatalf("ParseSFZ error = %v", err)
		}
		if got == nil {
			t.Fatal("ParseSFZ returned nil file with nil error")
		}
	})
}
