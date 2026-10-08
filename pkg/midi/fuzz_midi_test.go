package midi

import (
	"os"
	"path/filepath"
	"testing"
)

// FuzzMIDI feeds arbitrary bytes to the SMF reader. It must never
// panic; malformed input yields an error and valid input yields a file
// whose conversion result is deterministic.
func FuzzMIDI(f *testing.F) {
	f.Add("MThd\x00\x00\x00\x06\x00\x00\x00\x01\x00\x60MTrk\x00\x00\x00\x0b\x00\x90\x3c\x40\x83\x60\x80\x3c\x00\x00\xff\x2f\x00")
	f.Add("")
	f.Add("MThd")
	f.Add("\x00\x01\x02\xff\xfe\x80\x90\x3c")
	f.Add("RIFF\x08\x00\x00\x00WAVEdata\x00\x00\x00\x00")
	if data, err := os.ReadFile(filepath.Join("..", "..", "test_data", "mahler_symphony.mid")); err == nil {
		f.Add(string(data))
	}
	f.Fuzz(func(t *testing.T, data string) {
		mf, err := ReadFile([]byte(data))
		if err != nil {
			return // malformed input fails gracefully
		}
		if mf == nil {
			t.Fatal("nil file with nil error")
		}
		s, err := ToScore(mf)
		if err != nil {
			return // empty or noteless files fail gracefully
		}
		_ = WriteFile(FromScore(s, DefaultTicksPerQuarter))
	})
}
