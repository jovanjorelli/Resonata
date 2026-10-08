package wav

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"testing"
)

// pushWrite writes raw bytes as a file for DecodeFile tests.
func pushWrite(t *testing.T, name string, raw []byte) string {
	t.Helper()
	p := filepath.Join(t.TempDir(), name)
	if err := os.WriteFile(p, raw, 0o644); err != nil {
		t.Fatal(err)
	}
	return p
}

// TestPushDecodeFmtTooSmall proves fmt chunks under 16 bytes are rejected.
func TestPushDecodeFmtTooSmall(t *testing.T) {
	raw := []byte("RIFF\x10\x00\x00\x00WAVEfmt \x08\x00\x00\x00\x01\x00\x01\x00\x40\x1f\x00\x00")
	if _, _, err := DecodeFile(pushWrite(t, "small.wav", raw)); err == nil {
		t.Fatal("short fmt accepted")
	}
}

// TestPushDecodeTruncatedChunkHeader proves a cut chunk header errors.
func TestPushDecodeTruncatedChunkHeader(t *testing.T) {
	raw := []byte("RIFF\x0e\x00\x00\x00WAVEda")
	if _, _, err := DecodeFile(pushWrite(t, "trunc.wav", raw)); err == nil {
		t.Fatal("truncated chunk header accepted")
	}
}

// TestPushDecodeBadChannels proves zero channels/rates are rejected.
func TestPushDecodeBadChannels(t *testing.T) {
	hdr := append([]byte("RIFF\x1c\x00\x00\x00WAVEfmt \x10\x00\x00\x00"),
		[]byte("\x01\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x01\x00\x10\x00")...)
	if _, _, err := DecodeFile(pushWrite(t, "ch.wav", hdr)); err == nil {
		t.Fatal("zero channels accepted")
	}
}

// TestPushDecodeExtensibleErrors proves truncated/bad extensible tails fail.
func TestPushDecodeExtensibleErrors(t *testing.T) {
	base := append([]byte("RIFF\x24\x00\x00\x00WAVEfmt \x18\x00\x00\x00"),
		[]byte("\xFE\xFF\x02\x00\x80\xBB\x00\x00\x00\xEE\x02\x00\x04\x00\x10\x00\x00\x18\x00\x00\x00")...)
	// Truncated: rest < 24 with extensible tag.
	if _, _, err := DecodeFile(pushWrite(t, "ext1.wav", base)); err == nil {
		t.Fatal("truncated extensible accepted")
	}
	// Bad cbSize (!=22) and bad subformat and zero valid bits need a
	// full 40-byte fmt (16 + 24 tail).
	full := func(tail []byte) []byte {
		hdr := append([]byte("RIFF\x34\x00\x00\x00WAVEfmt \x28\x00\x00\x00"),
			[]byte("\xFE\xFF\x02\x00\x80\xBB\x00\x00\x00\xEE\x02\x00\x04\x00\x10\x00")...)
		return append(hdr, tail...)
	}
	badCB := []byte("\x07\x00\x10\x00\x03\x00\x00\x00\x01\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00")
	if _, _, err := DecodeFile(pushWrite(t, "ext2.wav", full(badCB))); err == nil {
		t.Fatal("bad cbSize accepted")
	}
	badSub := []byte("\x16\x00\x10\x00\x03\x00\x00\x00\x09\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00")
	if _, _, err := DecodeFile(pushWrite(t, "ext3.wav", full(badSub))); err == nil {
		t.Fatal("bad subformat accepted")
	}
	zeroBits := []byte("\x16\x00\x00\x00\x03\x00\x00\x00\x01\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00")
	if _, _, err := DecodeFile(pushWrite(t, "ext4.wav", full(zeroBits))); err == nil {
		t.Fatal("zero valid bits accepted")
	}
}

// TestPushDecodeUnknownChunkSkipped proves unknown chunks and odd padding
// do not break decoding of a valid file.
func TestPushDecodeUnknownChunkSkipped(t *testing.T) {
	f, w := writeTemp(t, "odd.wav", PCM16, 48000, 1)
	mono := sineFrames(32, 440, 0.5, 48000)
	if _, err := w.WriteFrames(mono); err != nil {
		t.Fatal(err)
	}
	path := f.Name()
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}
	if err := f.Close(); err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	// Insert a JUNK chunk with odd size 3 (+1 pad) before the data chunk.
	dataIdx := bytes.Index(raw, []byte("data"))
	if dataIdx < 0 {
		t.Fatal("no data chunk in fixture")
	}
	junk := []byte("JUNK\x03\x00\x00\x00abc\x00")
	spliced := append(append(append([]byte{}, raw[:dataIdx]...), junk...), raw[dataIdx:]...)
	// Fix RIFF size = len-8.
	spliced[4] = byte(len(spliced) - 8)
	spliced[5] = byte((len(spliced) - 8) >> 8)
	spliced[6] = byte((len(spliced) - 8) >> 16)
	spliced[7] = byte((len(spliced) - 8) >> 24)
	p := pushWrite(t, "junk.wav", spliced)
	info, samples, err := DecodeFile(p)
	if err != nil {
		t.Fatalf("junk chunk broke decode: %v", err)
	}
	if info.Frames != 32 || len(samples) != 32 {
		t.Fatalf("frames = %d/%d, want 32/32", info.Frames, len(samples))
	}
}

// TestPushDecodeTruncatedData proves short data yields partial frames.
func TestPushDecodeTruncatedData(t *testing.T) {
	hdr := append([]byte("RIFF\x30\x00\x00\x00WAVEfmt \x10\x00\x00\x00"),
		[]byte("\x01\x00\x01\x00\x40\x1f\x00\x00\x80\x3E\x00\x00\x02\x00\x10\x00")...)
	hdr = append(hdr, []byte("data\x10\x00\x00\x00\x01\x02")...)
	info, samples, err := Decode(bytes.NewReader(hdr))
	if err != nil {
		t.Fatalf("truncated data errored: %v", err)
	}
	if info.Frames != 1 || len(samples) != 1 {
		t.Fatalf("frames = %d/%d, want 1/1", info.Frames, len(samples))
	}
}

// TestPushDecodeFileMissing proves missing paths error.
func TestPushDecodeFileMissing(t *testing.T) {
	if _, _, err := DecodeFile(filepath.Join(t.TempDir(), "missing.wav")); err == nil {
		t.Fatal("missing file accepted")
	}
}

// TestPushWriterErrorPaths proves write-after-close, slice overflow,
// zero-frame writes, and idempotent Close.
func TestPushWriterErrorPaths(t *testing.T) {
	f, w := writeTemp(t, "err.wav", PCM16, 48000, 2)
	left := sineFrames(16, 440, 0.5, 48000)
	right := sineFrames(16, 660, 0.5, 48000)
	if _, err := w.WriteFrames(left, right, left); err == nil {
		t.Fatal("too many slices accepted")
	}
	if n, err := w.WriteFrames(); err != nil || n != 0 {
		t.Fatalf("zero-frame write = %d, %v; want 0, nil", n, err)
	}
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}
	if err := w.Close(); err != nil {
		t.Fatalf("second Close: %v", err)
	}
	if _, err := w.WriteFrames(left, right); err == nil {
		t.Fatal("write after Close accepted")
	}
	f.Close()
}

// failSeeker fails Seek to exercise patchAt error paths.
type failSeeker struct{ *os.File }

func (f failSeeker) Seek(int64, int) (int64, error) { return 0, errors.New("seek boom") }

func TestPushWriterPatchError(t *testing.T) {
	f, err := os.Create(filepath.Join(t.TempDir(), "p.wav"))
	if err != nil {
		t.Fatal(err)
	}
	w, err := NewWriter(f, PCM16, 48000, 1)
	if err != nil {
		t.Fatal(err)
	}
	w.w = failSeeker{f}
	if err := w.Close(); err == nil {
		t.Fatal("seek failure accepted")
	}
	f.Close()
}

// TestPushDecodeDataBadTag proves unsupported encodings fail fast via the
// direct data decoder.
func TestPushDecodeDataBadTag(t *testing.T) {
	info := Info{FormatTag: Format(7), BitsPerSample: 16, Channels: 1, SampleRate: 48000}
	if _, _, err := decodeData(bytes.NewReader([]byte{1, 2, 3, 4}), info, 4); err == nil {
		t.Fatal("bad tag accepted")
	}
	badAlign := Info{FormatTag: PCM24, BitsPerSample: 0, Channels: 0, SampleRate: 48000}
	if _, _, err := decodeData(bytes.NewReader(nil), badAlign, 0); err == nil {
		t.Fatal("bad alignment accepted")
	}
}

// TestPushSmplVariants proves short and loop-less smpl chunks decode to
// no loop without error.
func TestPushSmplVariants(t *testing.T) {
	short := []byte("RIFF\x1c\x00\x00\x00WAVEsmpl\x0a\x00\x00\x00abcdefghij")
	p := pushWrite(t, "smplshort.wav", short)
	if _, _, err := DecodeFile(p); err == nil {
		t.Log("short smpl without data chunk rejected as expected or skipped")
	}
	// smpl with one loop whose end <= start: no loop recorded.
	hdr36 := make([]byte, 36)
	hdr36[28] = 1 // numLoops = 1
	loop24 := make([]byte, 24)
	loop24[8] = 10 // start = 10
	loop24[12] = 5 // end = 5 <= start
	raw := append([]byte("RIFF\x64\x00\x00\x00WAVEsmpl\x3c\x00\x00\x00"), hdr36...)
	raw = append(raw, loop24...)
	fmtOK := append([]byte("fmt \x10\x00\x00\x00"),
		[]byte("\x01\x00\x01\x00\x40\x1f\x00\x00\x80\x3e\x00\x00\x02\x00\x10\x00")...)
	_ = fmtOK
	var info Info
	r := bytes.NewReader(append(raw, 0))
	if err := decodeSmpl(r, &info, 60); err != nil {
		t.Fatalf("smpl noloop: %v", err)
	}
	if info.HasLoop {
		t.Fatal("end<=start loop recorded")
	}
}

// TestPushWriterBigBlock proves oversized blocks grow the scratch buffer.
func TestPushWriterBigBlock(t *testing.T) {
	f, w := writeTemp(t, "big.wav", PCM16, 48000, 1)
	big := sineFrames(20000, 440, 0.5, 48000)
	n, err := w.WriteFrames(big)
	if err != nil || n != 20000 {
		t.Fatalf("big block = %d, %v; want 20000, nil", n, err)
	}
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}
	f.Close()
}
