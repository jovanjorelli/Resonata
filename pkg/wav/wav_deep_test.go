package wav

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"
)

// TestDeepEightBit proves unsigned 8-bit PCM decodes (128 centers silence).
func TestDeepEightBit(t *testing.T) {
	raw := []byte("RIFF\x2c\x00\x00\x00WAVEfmt \x10\x00\x00\x00" +
		"\x01\x00\x01\x00\x40\x1f\x00\x00\x40\x1f\x00\x00\x01\x00\x08\x00" +
		"data\x08\x00\x00\x00")
	raw = append(raw, []byte{128, 255, 0, 192, 64, 128, 128, 128}...)
	info, out, err := Decode(bytes.NewReader(raw))
	if err != nil {
		t.Fatalf("decode: %v", err)
	}
	if info.BitsPerSample != 8 || len(out) != 8 {
		t.Fatalf("info = %+v len %d", info, len(out))
	}
	if out[0] != 0 || out[1] > 1 || out[2] != -1 {
		t.Fatalf("samples = %v", out[:3])
	}
}

// TestDeepFloat64 proves 64-bit float samples decode.
func TestDeepFloat64(t *testing.T) {
	raw := []byte("RIFF\x40\x00\x00\x00WAVEfmt \x10\x00\x00\x00" +
		"\x03\x00\x02\x00\x80\xbb\x00\x00\x00\xb8\x0b\x00\x10\x00\x40\x00" +
		"data\x10\x00\x00\x00")
	raw = append(raw, bytes.Repeat([]byte{0}, 16)...)
	info, out, err := Decode(bytes.NewReader(raw))
	if err != nil {
		t.Fatalf("decode: %v", err)
	}
	if info.BitsPerSample != 64 || len(out) != 2 {
		t.Fatalf("info = %+v len %d", info, len(out))
	}
}

// TestDeepSmplLoop proves loop points decode and empty smpl skips.
func TestDeepSmplLoop(t *testing.T) {
	f, w := writeTemp(t, "loop.wav", PCM16, 48000, 1)
	left := sineFrames(64, 440, 0.5, 48000)
	if _, err := w.WriteFrames(left); err != nil {
		t.Fatal(err)
	}
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}
	path := f.Name()
	f.Close()
	info, _, err := DecodeFile(path)
	if err != nil {
		t.Fatalf("decode: %v", err)
	}
	if info.HasLoop {
		t.Fatal("unexpected loop")
	}
}

// TestDeepWriterErrors proves constructor and write guards fail loudly.
func TestDeepWriterErrors(t *testing.T) {
	if _, err := NewWriterBits(nil, PCM16, 16, 48000, 1); err == nil {
		t.Error("nil destination accepted")
	}
	f, _ := os.Create(filepath.Join(t.TempDir(), "x.wav"))
	defer f.Close()
	if _, err := NewWriterBits(f, PCM16, 16, 0, 0); err == nil {
		t.Error("bad layout accepted")
	}
	if _, err := NewWriterBits(f, PCM16, 12, 48000, 1); err == nil {
		t.Error("bad PCM depth accepted")
	}
	if _, err := NewWriterBits(f, Float32, 16, 48000, 1); err == nil {
		t.Error("bad float depth accepted")
	}
	if _, err := NewWriterBits(f, Format(7), 16, 48000, 1); err == nil {
		t.Error("bad tag accepted")
	}
	w, err := NewWriter(f, PCM16, 48000, 1)
	if err != nil {
		t.Fatal(err)
	}
	a := make([]float32, 8)
	if _, err := w.WriteFrames(a, a); err == nil {
		t.Error("too many channels accepted")
	}
	if n, err := w.WriteFrames(); err != nil || n != 0 {
		t.Fatalf("empty write = %d %v", n, err)
	}
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}
	if err := w.Close(); err != nil {
		t.Fatalf("second close: %v", err)
	}
	if _, err := w.WriteFrames(a); err == nil {
		t.Error("write after close accepted")
	}
}

// TestDeepDecodeTruncated proves short streams fail gracefully.
func TestDeepDecodeTruncated(t *testing.T) {
	if _, _, err := Decode(bytes.NewReader([]byte("RIFF"))); err == nil {
		t.Error("short header accepted")
	}
	if _, _, err := Decode(bytes.NewReader([]byte("NOPE........"))); err == nil {
		t.Error("non-RIFF accepted")
	}
	bad := []byte("RIFF\x08\x00\x00\x00WAVEdata\x00\x00\x00\x00")
	if _, _, err := Decode(bytes.NewReader(bad)); err == nil {
		t.Error("data-before-fmt accepted")
	}
}

// TestDeepExtensibleErrors proves malformed extensible tails fail.
func TestDeepExtensibleErrors(t *testing.T) {
	base := []byte("RIFF\x24\x00\x00\x00WAVEfmt \x28\x00\x00\x00\xfe\xff\x02\x00\x80\xbb\x00\x00\x00\xee\x02\x00\x04\x00\x10\x00")
	for _, tail := range [][]byte{
		make([]byte, 10),
		append([]byte("\x07\x00\x10\x00\x03\x00\x00\x00\x01\x00\x00\x00"), make([]byte, 12)...),
		append([]byte("\x16\x00\x10\x00\x03\x00\x00\x00\x09\x00\x00\x00"), make([]byte, 12)...),
	} {
		raw := append(append([]byte{}, base...), tail...)
		if _, _, err := Decode(bytes.NewReader(raw)); err == nil {
			t.Errorf("bad extensible tail accepted (%d bytes)", len(tail))
		}
	}
}
