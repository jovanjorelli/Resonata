package wav

import (
	"encoding/binary"
	"math"
	"os"
	"path/filepath"
	"testing"
)

// smplFile builds a minimal 16-bit stereo WAV with a smpl chunk holding
// one loop from frame 1 to frame 3.
func smplFile(t *testing.T, path string) {
	t.Helper()
	var buf []byte
	u16 := func(v uint16) { buf = binary.LittleEndian.AppendUint16(buf, v) }
	u32 := func(v uint32) { buf = binary.LittleEndian.AppendUint32(buf, v) }
	data := []int16{1000, -1000, 2000, -2000, 3000, -3000, 4000, -4000}
	buf = append(buf, "RIFF"...)
	u32(4 + 24 + 8 + uint32(len(data))*2 + 8 + 60)
	buf = append(buf, "WAVE"...)
	buf = append(buf, "fmt "...)
	u32(16)
	u16(1)
	u16(2)
	u32(44100)
	u32(44100 * 4)
	u16(4)
	u16(16)
	buf = append(buf, "data"...)
	u32(uint32(len(data)) * 2)
	for _, v := range data {
		u16(uint16(v))
	}
	buf = append(buf, "smpl"...)
	u32(60)
	for i := 0; i < 7; i++ {
		u32(0)
	}
	u32(1)
	u32(0)
	u32(0)
	u32(0)
	u32(1)
	u32(3)
	u32(0)
	u32(0)
	if err := os.WriteFile(path, buf, 0o644); err != nil {
		t.Fatal(err)
	}
}

// TestDecodeSmplLoop checks smpl loop extraction and 16-bit scaling.
func TestDecodeSmplLoop(t *testing.T) {
	path := filepath.Join(t.TempDir(), "loop.wav")
	smplFile(t, path)
	info, out, err := DecodeFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !info.HasLoop || info.LoopStart != 1 || info.LoopEnd != 3 {
		t.Fatalf("loop = %+v", info)
	}
	if len(out) != 8 || out[0] != float32(1000)/32768 {
		t.Fatalf("samples = %v", out)
	}
	// A short smpl chunk skips cleanly without a loop.
	short := func() []byte {
		var buf []byte
		u16 := func(v uint16) { buf = binary.LittleEndian.AppendUint16(buf, v) }
		u32 := func(v uint32) { buf = binary.LittleEndian.AppendUint32(buf, v) }
		buf = append(buf, "RIFF"...)
		u32(4 + 24 + 8 + 4 + 8 + 4)
		buf = append(buf, "WAVE"...)
		buf = append(buf, "fmt "...)
		u32(16)
		u16(1)
		u16(1)
		u32(8000)
		u32(8000 * 2)
		u16(2)
		u16(16)
		buf = append(buf, "data"...)
		u32(4)
		buf = append(buf, 0, 0, 0, 0)
		buf = append(buf, "smpl"...)
		u32(4)
		u32(0)
		return buf
	}
	sp := filepath.Join(t.TempDir(), "short.wav")
	if err := os.WriteFile(sp, short(), 0o644); err != nil {
		t.Fatal(err)
	}
	info, _, err = DecodeFile(sp)
	if err != nil {
		t.Fatal(err)
	}
	if info.HasLoop {
		t.Fatalf("short smpl reported %+v", info)
	}
}

// TestFloatToIntEdges covers NaN and out-of-range clamping on every
// integer encoder.
func TestFloatToIntEdges(t *testing.T) {
	nan := float32(math.NaN())
	if floatToInt16(nan) != 0 || floatToInt24(nan) != 0 || floatToInt32(nan) != 0 {
		t.Fatal("NaN should map to 0")
	}
	if floatToInt16(2) != int16Max || floatToInt16(-2) != -int16Max {
		t.Fatalf("int16 clamp = %d/%d", floatToInt16(2), floatToInt16(-2))
	}
	if floatToInt24(2) != int24Max || floatToInt24(-2) != -int24Max {
		t.Fatalf("int24 clamp = %d/%d", floatToInt24(2), floatToInt24(-2))
	}
	if floatToInt32(2) != int32Max || floatToInt32(-2) != -int32Max {
		t.Fatalf("int32 clamp = %d/%d", floatToInt32(2), floatToInt32(-2))
	}
	if floatToInt16(0.5) != 16384 && floatToInt16(0.5) != 16383 {
		t.Fatalf("int16(0.5) = %d", floatToInt16(0.5))
	}
}

// TestWriterAccessors covers the reporting methods and the
// constructor's bit-depth validation.
func TestWriterAccessors(t *testing.T) {
	f, w := writeTemp(t, "acc.wav", PCM24, 48000, 2)
	if w.SampleRate() != 48000 || w.Channels() != 2 || w.Frames() != 0 {
		t.Fatalf("fresh writer = %d/%d/%d", w.SampleRate(), w.Channels(), w.Frames())
	}
	buf := sineFrames(10, 440, 0.5, 48000)
	if n, err := w.WriteFrames(buf, buf); err != nil || n != 10 {
		t.Fatal(err)
	}
	if w.Frames() != 10 {
		t.Fatalf("frames = %d, want 10", w.Frames())
	}
	if _, err := w.WriteFrames(); err != nil {
		t.Fatal(err)
	}
	if w.Frames() != 10 {
		t.Fatalf("empty write moved frames to %d", w.Frames())
	}
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}
	if err := w.Close(); err != nil { // idempotent
		t.Fatal(err)
	}
	path := f.Name()
	f.Close()
	dir := t.TempDir()
	for _, tc := range []struct {
		name   string
		format Format
		bits   int
	}{
		{"pcm20", PCM24, 20},
		{"flt64", Float32, 64},
		{"tag7", Format(7), 16},
	} {
		f, err := os.Create(filepath.Join(dir, tc.name+".wav"))
		if err != nil {
			t.Fatal(err)
		}
		if _, err := NewWriterBits(f, tc.format, tc.bits, 48000, 2); err == nil {
			t.Errorf("%s: want error, got nil", tc.name)
		}
		f.Close()
	}
	_ = path
}

// TestChannelMaskTable checks every speaker mask branch.
func TestChannelMaskTable(t *testing.T) {
	cases := map[int]uint32{1: 0x4, 2: 0x3, 3: 0x7, 4: 0x33, 5: 0x37, 6: 0x3F, 7: 0x7F, 8: 0xFF}
	for ch, want := range cases {
		if got := channelMask(ch); got != want {
			t.Errorf("mask(%d) = %#x, want %#x", ch, got, want)
		}
	}
	if got := channelMask(40); got != 0xFFFFFFFF {
		t.Errorf("mask(40) = %#x, want saturated", got)
	}
}

// TestDecodeErrors checks malformed inputs fail with errors, not panics.
func TestDecodeErrors(t *testing.T) {
	dir := t.TempDir()
	write := func(name string, raw []byte) string {
		p := filepath.Join(dir, name)
		if err := os.WriteFile(p, raw, 0o644); err != nil {
			t.Fatal(err)
		}
		return p
	}
	if _, _, err := DecodeFile(write("short.wav", []byte("RIFF"))); err == nil {
		t.Error("short header accepted")
	}
	if _, _, err := DecodeFile(write("noriff.wav", []byte("NOPE........"))); err == nil {
		t.Error("non-RIFF accepted")
	}
	// data chunk before fmt.
	bad := []byte("RIFF\x08\x00\x00\x00WAVEdata\x00\x00\x00\x00")
	if _, _, err := DecodeFile(write("order.wav", bad)); err == nil {
		t.Error("data-before-fmt accepted")
	}
	// fmt only, no data.
	nodata := append([]byte("RIFF\x1c\x00\x00\x00WAVEfmt \x10\x00\x00\x00"),
		[]byte("\x01\x00\x01\x00\x40\x1f\x00\x00\x80\x3e\x00\x00\x02\x00\x10\x00")...)
	if _, _, err := DecodeFile(write("nodata.wav", nodata)); err == nil {
		t.Error("missing data accepted")
	}
	// Unsupported format tag and truncated extensible header.
	for i, tag := range []uint16{7, 0xFFFE} {
		hdr := append([]byte("RIFF\x1c\x00\x00\x00WAVEfmt \x10\x00\x00\x00"),
			byte(tag), byte(tag>>8))
		hdr = append(hdr, []byte("\x01\x00\x40\x1f\x00\x00\x80\x3e\x00\x00\x02\x00\x10\x00")...)
		if _, _, err := DecodeFile(write(string(rune('a'+i))+".wav", hdr)); err == nil {
			t.Errorf("tag %d accepted", tag)
		}
	}
}

// chunkFile builds fmt/data headers around the given payloads for
// depth-validation tests.
func chunkFile(t *testing.T, path string, tag uint16, bits uint16) {
	t.Helper()
	hdr := append([]byte("RIFF\x1c\x00\x00\x00WAVEfmt \x10\x00\x00\x00"),
		byte(tag), byte(tag>>8))
	hdr = append(hdr, []byte("\x02\x00\x80\xbb\x00\x00\x00\xee\x02\x00\x04\x00")...)
	hdr = append(hdr, byte(bits), byte(bits>>8))
	if err := os.WriteFile(path, hdr, 0o644); err != nil {
		t.Fatal(err)
	}
}

// TestValidateFmtDepths checks unsupported bit depths are rejected.
func TestValidateFmtDepths(t *testing.T) {
	dir := t.TempDir()
	for _, tc := range []struct {
		tag  uint16
		bits uint16
	}{
		{1, 12},
		{3, 16},
	} {
		p := filepath.Join(dir, "depth.wav")
		chunkFile(t, p, tc.tag, tc.bits)
		if _, _, err := DecodeFile(p); err == nil {
			t.Errorf("tag %d/%d accepted", tc.tag, tc.bits)
		}
	}
}
