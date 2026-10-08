package wav

import (
	"bytes"
	"testing"
)

// FuzzWAV feeds arbitrary bytes to the RIFF WAVE decoder. It must never
// panic; malformed input yields an error and valid input yields decoded
// frames within [-1, 1].
func FuzzWAV(f *testing.F) {
	f.Add("RIFF\x24\x00\x00\x00WAVEfmt \x10\x00\x00\x00\x01\x00\x01\x00\x40\x1f\x00\x00\x80\x3e\x00\x00\x02\x00\x10\x00data\x04\x00\x00\x00\x00\x00\x00\x00")
	f.Add("")
	f.Add("RIFF")
	f.Add("NOPE........")
	f.Add("\x00\x01\x02\xff\xfe")
	f.Fuzz(func(t *testing.T, data string) {
		info, out, err := Decode(bytes.NewReader([]byte(data)))
		if err != nil {
			return // malformed input fails gracefully
		}
		if info.Frames != len(out)/max(info.Channels, 1) {
			t.Fatalf("frames %d != samples/channels %d", info.Frames, len(out))
		}
		for i, v := range out {
			if v < -1.001 || v > 1.001 {
				t.Fatalf("sample %d = %v out of range", i, v)
			}
		}
	})
}
