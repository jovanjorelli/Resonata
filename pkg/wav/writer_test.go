package wav

import (
	"encoding/binary"
	"math"
	"os"
	"path/filepath"
	"testing"
)

// Note: integer PCM (16/24/32 bit, mono/stereo) uses the plain 16-byte
// fmt chunk (44-byte header). Float encodings and anything above two
// channels use WAVE_FORMAT_EXTENSIBLE with a 40-byte fmt chunk carrying
// cbSize 22, valid bits, channel mask, and SubFormat GUID (68-byte
// header, no fact chunk).

// readRaw reads the whole file at path.
func readRaw(t *testing.T, path string) []byte {
	t.Helper()
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return raw
}

// writeTemp creates a writer over a temp file and returns both. The
// caller closes the writer, then the file.
func writeTemp(t *testing.T, name string, format Format, sampleRate, channels int) (*os.File, *Writer) {
	t.Helper()
	f, err := os.Create(filepath.Join(t.TempDir(), name))
	if err != nil {
		t.Fatal(err)
	}
	w, err := NewWriter(f, format, sampleRate, channels)
	if err != nil {
		f.Close()
		t.Fatal(err)
	}
	return f, w
}

// checkRIFF verifies the outer container: magic bytes, WAVE identifier,
// and the RIFF size equal to file size minus 8.
func checkRIFF(t *testing.T, raw []byte) {
	t.Helper()
	if string(raw[0:4]) != "RIFF" {
		t.Fatalf("RIFF magic = %q, want %q", raw[0:4], "RIFF")
	}
	if string(raw[8:12]) != "WAVE" {
		t.Fatalf("WAVE id = %q, want %q", raw[8:12], "WAVE")
	}
	if got, want := binary.LittleEndian.Uint32(raw[4:8]), uint32(len(raw)-8); got != want {
		t.Fatalf("RIFF size = %d, want file size - 8 = %d", got, want)
	}
}

// checkFmt16 verifies a 16-byte PCM fmt chunk starting at offset 12 and
// returns the offset of the next chunk header.
func checkFmt16(t *testing.T, raw []byte, format, channels, sampleRate, bits int) int {
	t.Helper()
	if string(raw[12:16]) != "fmt " {
		t.Fatalf("fmt id = %q, want %q", raw[12:16], "fmt ")
	}
	if got := binary.LittleEndian.Uint32(raw[16:20]); got != 16 {
		t.Fatalf("fmt length = %d, want 16", got)
	}
	if got := binary.LittleEndian.Uint16(raw[20:22]); int(got) != format {
		t.Fatalf("format code = %d, want %d", got, format)
	}
	if got := binary.LittleEndian.Uint16(raw[22:24]); int(got) != channels {
		t.Fatalf("channels = %d, want %d", got, channels)
	}
	if got := binary.LittleEndian.Uint32(raw[24:28]); int(got) != sampleRate {
		t.Fatalf("sample rate = %d, want %d", got, sampleRate)
	}
	blockAlign := channels * bits / 8
	if got := binary.LittleEndian.Uint32(raw[28:32]); int(got) != sampleRate*blockAlign {
		t.Fatalf("byte rate = %d, want %d", got, sampleRate*blockAlign)
	}
	if got := binary.LittleEndian.Uint16(raw[32:34]); int(got) != blockAlign {
		t.Fatalf("block align = %d, want %d", got, blockAlign)
	}
	if got := binary.LittleEndian.Uint16(raw[34:36]); int(got) != bits {
		t.Fatalf("bits per sample = %d, want %d", got, bits)
	}
	return 36
}

// checkData verifies the data chunk header at off and returns its size.
func checkData(t *testing.T, raw []byte, off int) uint32 {
	t.Helper()
	if string(raw[off:off+4]) != "data" {
		t.Fatalf("data id at %d = %q, want %q", off, raw[off:off+4], "data")
	}
	return binary.LittleEndian.Uint32(raw[off+4 : off+8])
}

// sineFrames renders n frames of a sine at freq Hz and amplitude amp.
func sineFrames(n int, freq, amp float64, sampleRate int) []float32 {
	out := make([]float32, n)
	for i := range out {
		out[i] = float32(amp * math.Sin(2*math.Pi*freq*float64(i)/float64(sampleRate)))
	}
	return out
}

// TestWriterHeaderPCM16 checks every header field of a 16-bit stereo
// file with known samples.
func TestWriterHeaderPCM16(t *testing.T) {
	const sr, ch, frames = 48000, 2, 100
	f, w := writeTempBits(t, "p16.wav", PCM16, 16, sr, ch)
	left := sineFrames(frames, 440, 0.5, sr)
	right := sineFrames(frames, 660, 0.5, sr)
	if n, err := w.WriteFrames(left, right); err != nil || n != frames {
		t.Fatalf("WriteFrames = %d, %v; want %d, nil", n, err, frames)
	}
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}
	path := f.Name()
	if err := f.Close(); err != nil {
		t.Fatal(err)
	}

	raw := readRaw(t, path)
	checkRIFF(t, raw)
	off := checkFmt16(t, raw, 1, ch, sr, 16)
	if got, want := checkData(t, raw, off), uint32(frames*ch*2); got != want {
		t.Fatalf("data size = %d, want %d", got, want)
	}
	if want := 44 + frames*ch*2; len(raw) != want {
		t.Fatalf("file size = %d, want 44 + data = %d", len(raw), want)
	}
}

// writeTempBits creates a writer with an explicit bit depth.
func writeTempBits(t *testing.T, name string, format Format, bits, sampleRate, channels int) (*os.File, *Writer) {
	t.Helper()
	f, err := os.Create(filepath.Join(t.TempDir(), name))
	if err != nil {
		t.Fatal(err)
	}
	w, err := NewWriterBits(f, format, bits, sampleRate, channels)
	if err != nil {
		f.Close()
		t.Fatal(err)
	}
	return f, w
}

// TestWriterHeaderPCM24 checks every header field of a 24-bit stereo
// file with known samples.
func TestWriterHeaderPCM24(t *testing.T) {
	const sr, ch, frames = 48000, 2, 100
	f, w := writeTemp(t, "p24.wav", PCM24, sr, ch)
	left := sineFrames(frames, 440, 0.5, sr)
	right := sineFrames(frames, 660, 0.5, sr)
	if n, err := w.WriteFrames(left, right); err != nil || n != frames {
		t.Fatalf("WriteFrames = %d, %v; want %d, nil", n, err, frames)
	}
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}
	path := f.Name()
	if err := f.Close(); err != nil {
		t.Fatal(err)
	}

	raw := readRaw(t, path)
	checkRIFF(t, raw)
	off := checkFmt16(t, raw, 1, ch, sr, 24)
	if got, want := checkData(t, raw, off), uint32(frames*ch*3); got != want {
		t.Fatalf("data size = %d, want %d", got, want)
	}
	if want := 44 + frames*ch*3; len(raw) != want {
		t.Fatalf("file size = %d, want 44 + data = %d", len(raw), want)
	}
}

// TestWriterHeaderPCM32 checks the 32-bit integer PCM layout: format
// tag 1 with 4 bytes per sample.
func TestWriterHeaderPCM32(t *testing.T) {
	const sr, ch, frames = 48000, 2, 100
	f, w := writeTempBits(t, "p32.wav", PCM32, 32, sr, ch)
	left := sineFrames(frames, 440, 0.5, sr)
	right := sineFrames(frames, 660, 0.5, sr)
	if n, err := w.WriteFrames(left, right); err != nil || n != frames {
		t.Fatalf("WriteFrames = %d, %v; want %d, nil", n, err, frames)
	}
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}
	path := f.Name()
	if err := f.Close(); err != nil {
		t.Fatal(err)
	}

	raw := readRaw(t, path)
	checkRIFF(t, raw)
	off := checkFmt16(t, raw, 1, ch, sr, 32)
	if got, want := checkData(t, raw, off), uint32(frames*ch*4); got != want {
		t.Fatalf("data size = %d, want %d", got, want)
	}
	if want := 44 + frames*ch*4; len(raw) != want {
		t.Fatalf("file size = %d, want 44 + data = %d", len(raw), want)
	}
}

// checkExtensible verifies a 40-byte extensible fmt chunk at offset 12:
// tag 0xFFFE, cbSize 22, valid bits, channel mask, and SubFormat GUID.
// subWant is 1 for PCM or 3 for float. It returns the data chunk offset.
func checkExtensible(t *testing.T, raw []byte, subWant uint16, channels, sampleRate, bits int, mask uint32) int {
	t.Helper()
	if string(raw[12:16]) != "fmt " {
		t.Fatalf("fmt id = %q", raw[12:16])
	}
	if got := binary.LittleEndian.Uint32(raw[16:20]); got != 40 {
		t.Fatalf("fmt length = %d, want 40", got)
	}
	if got := binary.LittleEndian.Uint16(raw[20:22]); got != 0xFFFE {
		t.Fatalf("format code = %#x, want 0xFFFE", got)
	}
	if got := binary.LittleEndian.Uint16(raw[22:24]); int(got) != channels {
		t.Fatalf("channels = %d, want %d", got, channels)
	}
	if got := binary.LittleEndian.Uint32(raw[24:28]); int(got) != sampleRate {
		t.Fatalf("sample rate = %d, want %d", got, sampleRate)
	}
	bytesPer := bits / 8
	if got := binary.LittleEndian.Uint32(raw[28:32]); int(got) != sampleRate*channels*bytesPer {
		t.Fatalf("byte rate = %d, want %d", got, sampleRate*channels*bytesPer)
	}
	if got := binary.LittleEndian.Uint16(raw[32:34]); int(got) != channels*bytesPer {
		t.Fatalf("block align = %d, want %d", got, channels*bytesPer)
	}
	if got := binary.LittleEndian.Uint16(raw[34:36]); int(got) != bits {
		t.Fatalf("bits per sample = %d, want %d", got, bits)
	}
	if got := binary.LittleEndian.Uint16(raw[36:38]); got != 22 {
		t.Fatalf("cbSize = %d, want 22", got)
	}
	if got := binary.LittleEndian.Uint16(raw[38:40]); int(got) != bits {
		t.Fatalf("valid bits = %d, want %d", got, bits)
	}
	if got := binary.LittleEndian.Uint32(raw[40:44]); got != mask {
		t.Fatalf("channel mask = %#x, want %#x", got, mask)
	}
	if got := binary.LittleEndian.Uint32(raw[44:48]); got != uint32(subWant) {
		t.Fatalf("SubFormat head = %#x, want %#x", got, subWant)
	}
	wantTail := []byte{0x00, 0x00, 0x10, 0x00, 0x80, 0x00, 0x00, 0xAA, 0x00, 0x38, 0x9B, 0x71}
	for i, b := range wantTail {
		if raw[48+i] != b {
			t.Fatalf("SubFormat GUID tail byte %d = %#x, want %#x", i, raw[48+i], b)
		}
	}
	return 60
}

// TestWriterHeaderFloat32 checks the 32-bit float stereo layout:
// extensible tag, float SubFormat GUID, valid bits 32, stereo mask.
func TestWriterHeaderFloat32(t *testing.T) {
	const sr, ch, frames = 48000, 2, 64
	f, w := writeTemp(t, "f32.wav", Float32, sr, ch)
	buf := sineFrames(frames, 440, 0.5, sr)
	if n, err := w.WriteFrames(buf, buf); err != nil || n != frames {
		t.Fatalf("WriteFrames = %d, %v; want %d, nil", n, err, frames)
	}
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}
	path := f.Name()
	if err := f.Close(); err != nil {
		t.Fatal(err)
	}

	raw := readRaw(t, path)
	checkRIFF(t, raw)
	off := checkExtensible(t, raw, 3, ch, sr, 32, 0x3)
	if got, want := checkData(t, raw, off), uint32(frames*ch*4); got != want {
		t.Fatalf("data size = %d, want %d", got, want)
	}
	if want := 68 + frames*ch*4; len(raw) != want {
		t.Fatalf("file size = %d, want 68 + data = %d", len(raw), want)
	}
}

// TestWriterEmpty checks finalizing without samples produces a valid
// zero-length file instead of an error.
func TestWriterEmpty(t *testing.T) {
	for _, tc := range []struct {
		name   string
		format Format
		header int
	}{
		{"empty24.wav", PCM24, 44},
		{"empty32.wav", Float32, 68},
	} {
		f, w := writeTemp(t, tc.name, tc.format, 48000, 2)
		if err := w.Close(); err != nil {
			t.Fatal(err)
		}
		path := f.Name()
		if err := f.Close(); err != nil {
			t.Fatal(err)
		}
		raw := readRaw(t, path)
		checkRIFF(t, raw)
		dataOff := 36
		if tc.format == Float32 {
			dataOff = 60
		}
		if got := checkData(t, raw, dataOff); got != 0 {
			t.Fatalf("%s: data size = %d, want 0", tc.name, got)
		}
		if len(raw) != tc.header {
			t.Fatalf("%s: file size = %d, want header only = %d", tc.name, len(raw), tc.header)
		}
		info, samples, err := DecodeFile(path)
		if err != nil {
			t.Fatal(err)
		}
		if info.Frames != 0 || len(samples) != 0 {
			t.Fatalf("%s: decoded %d frames, want 0", tc.name, info.Frames)
		}
	}
}

// TestWriterRoundTrip writes a 440 Hz sine and reads it back, comparing
// against the original within quantization tolerance.
func TestWriterRoundTrip(t *testing.T) {
	const sr, frames = 48000, 100
	// 24-bit: half-scale keeps float32 representation error well under
	// one quantization step (1 LSB = 1/8388607).
	f, w := writeTemp(t, "rt24.wav", PCM24, sr, 2)
	left := sineFrames(frames, 440, 0.5, sr)
	right := sineFrames(frames, 440, 0.25, sr)
	if _, err := w.WriteFrames(left, right); err != nil {
		t.Fatal(err)
	}
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}
	path := f.Name()
	if err := f.Close(); err != nil {
		t.Fatal(err)
	}
	info, back, err := DecodeFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if info.FormatTag != PCM24 || info.BitsPerSample != 24 || info.Channels != 2 || info.Frames != frames {
		t.Fatalf("decoded info = %+v", info)
	}
	const lsb = 1.0 / 8388607
	for i := 0; i < frames; i++ {
		if d := math.Abs(float64(back[2*i] - left[i])); d > lsb {
			t.Fatalf("24-bit L frame %d differs by %v (> 1 LSB)", i, d)
		}
		if d := math.Abs(float64(back[2*i+1] - right[i])); d > lsb {
			t.Fatalf("24-bit R frame %d differs by %v (> 1 LSB)", i, d)
		}
	}

	// 16-bit: one quantization step is 1/32767.
	f, w = writeTempBits(t, "rt16.wav", PCM16, 16, sr, 2)
	if _, err := w.WriteFrames(left, right); err != nil {
		t.Fatal(err)
	}
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}
	path = f.Name()
	if err := f.Close(); err != nil {
		t.Fatal(err)
	}
	info, back, err = DecodeFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if info.FormatTag != PCM16 || info.BitsPerSample != 16 || info.Channels != 2 || info.Frames != frames {
		t.Fatalf("decoded info = %+v", info)
	}
	const lsb16 = 1.0 / 32767
	for i := 0; i < frames; i++ {
		if d := math.Abs(float64(back[2*i] - left[i])); d > lsb16 {
			t.Fatalf("16-bit L frame %d differs by %v (> 1 LSB)", i, d)
		}
		if d := math.Abs(float64(back[2*i+1] - right[i])); d > lsb16 {
			t.Fatalf("16-bit R frame %d differs by %v (> 1 LSB)", i, d)
		}
	}

	// 32-bit integer: quantization is far below float32 precision, so
	// the round trip must match within float epsilon instead of 1 LSB.
	f, w = writeTempBits(t, "rt32i.wav", PCM32, 32, sr, 2)
	if _, err := w.WriteFrames(left, right); err != nil {
		t.Fatal(err)
	}
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}
	path = f.Name()
	if err := f.Close(); err != nil {
		t.Fatal(err)
	}
	info, back, err = DecodeFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if info.FormatTag != PCM32 || info.BitsPerSample != 32 || info.Channels != 2 || info.Frames != frames {
		t.Fatalf("decoded info = %+v", info)
	}
	for i := 0; i < frames; i++ {
		if d := math.Abs(float64(back[2*i] - left[i])); d > 1e-7 {
			t.Fatalf("32-bit L frame %d differs by %v", i, d)
		}
		if d := math.Abs(float64(back[2*i+1] - right[i])); d > 1e-7 {
			t.Fatalf("32-bit R frame %d differs by %v", i, d)
		}
	}

	// 32-bit float: bits survive exactly; require near-exact match.
	f, w = writeTemp(t, "rt32.wav", Float32, sr, 2)
	if _, err := w.WriteFrames(left, right); err != nil {
		t.Fatal(err)
	}
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}
	path = f.Name()
	if err := f.Close(); err != nil {
		t.Fatal(err)
	}
	info, back, err = DecodeFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if info.FormatTag != Float32 || info.BitsPerSample != 32 {
		t.Fatalf("decoded info = %+v", info)
	}
	for i := 0; i < frames; i++ {
		if d := math.Abs(float64(back[2*i] - left[i])); d > 1e-7 {
			t.Fatalf("float L frame %d differs by %v", i, d)
		}
		if d := math.Abs(float64(back[2*i+1] - right[i])); d > 1e-7 {
			t.Fatalf("float R frame %d differs by %v", i, d)
		}
	}
}

// TestWriterMonoStereo checks channel counts and that stereo channels
// stay separated.
func TestWriterMonoStereo(t *testing.T) {
	const sr, frames = 48000, 64
	mono := sineFrames(frames, 440, 0.5, sr)
	f, w := writeTemp(t, "mono.wav", PCM24, sr, 1)
	if _, err := w.WriteFrames(mono); err != nil {
		t.Fatal(err)
	}
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}
	monoPath := f.Name()
	if err := f.Close(); err != nil {
		t.Fatal(err)
	}
	raw := readRaw(t, monoPath)
	if got := binary.LittleEndian.Uint16(raw[22:24]); got != 1 {
		t.Fatalf("mono channels = %d, want 1", got)
	}
	if got := binary.LittleEndian.Uint16(raw[32:34]); got != 3 {
		t.Fatalf("mono block align = %d, want 3", got)
	}
	info, back, err := DecodeFile(monoPath)
	if err != nil {
		t.Fatal(err)
	}
	if info.Channels != 1 || info.Frames != frames {
		t.Fatalf("mono info = %+v", info)
	}
	const lsbMono = 1.0 / 8388607
	for i := 0; i < frames; i++ {
		if d := math.Abs(float64(back[i] - mono[i])); d > lsbMono {
			t.Fatalf("mono frame %d = %v, want %v", i, back[i], mono[i])
		}
	}

	left := sineFrames(frames, 440, 0.5, sr)
	right := sineFrames(frames, 440, -0.5, sr) // inverted: must not leak into L
	f, w = writeTemp(t, "stereo.wav", PCM24, sr, 2)
	if _, err := w.WriteFrames(left, right); err != nil {
		t.Fatal(err)
	}
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}
	stPath := f.Name()
	if err := f.Close(); err != nil {
		t.Fatal(err)
	}
	raw = readRaw(t, stPath)
	if got := binary.LittleEndian.Uint16(raw[22:24]); got != 2 {
		t.Fatalf("stereo channels = %d, want 2", got)
	}
	info, back, err = DecodeFile(stPath)
	if err != nil {
		t.Fatal(err)
	}
	const lsb = 1.0 / 8388607
	for i := 0; i < frames; i++ {
		if d := math.Abs(float64(back[2*i] - left[i])); d > lsb {
			t.Fatalf("stereo L frame %d = %v, want %v", i, back[2*i], left[i])
		}
		if d := math.Abs(float64(back[2*i+1] - right[i])); d > lsb {
			t.Fatalf("stereo R frame %d = %v, want %v", i, back[2*i+1], right[i])
		}
	}
}

// TestWriterFileSize checks total file size against header plus data for
// both encodings, catching off-by-one chunk arithmetic.
func TestWriterFileSize(t *testing.T) {
	const sr, ch, frames = 48000, 2, 257 // odd count exercises no padding path
	buf := sineFrames(frames, 440, 0.5, sr)
	f, w := writeTemp(t, "size24.wav", PCM24, sr, ch)
	if _, err := w.WriteFrames(buf, buf); err != nil {
		t.Fatal(err)
	}
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}
	path := f.Name()
	if err := f.Close(); err != nil {
		t.Fatal(err)
	}
	if fi, err := os.Stat(path); err != nil {
		t.Fatal(err)
	} else if want := int64(44 + frames*ch*3); fi.Size() != want {
		t.Fatalf("PCM file size = %d, want %d", fi.Size(), want)
	}

	f, w = writeTemp(t, "size32.wav", Float32, sr, ch)
	if _, err := w.WriteFrames(buf, buf); err != nil {
		t.Fatal(err)
	}
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}
	path = f.Name()
	if err := f.Close(); err != nil {
		t.Fatal(err)
	}
	if fi, err := os.Stat(path); err != nil {
		t.Fatal(err)
	} else if want := int64(68 + frames*ch*4); fi.Size() != want {
		t.Fatalf("float file size = %d, want %d", fi.Size(), want)
	}
}

// TestWriterRejectsBadLayout checks the constructor refuses unsupported
// format tags and layouts instead of emitting a corrupt header.
func TestWriterRejectsBadLayout(t *testing.T) {
	dir := t.TempDir()
	for _, tc := range []struct {
		name       string
		format     Format
		sampleRate int
		channels   int
	}{
		{"bad-tag", Format(0xFFFE), 48000, 2},
		{"bad-rate", PCM24, 0, 2},
		{"bad-channels", PCM24, 48000, 0},
	} {
		f, err := os.Create(filepath.Join(dir, tc.name+".wav"))
		if err != nil {
			t.Fatal(err)
		}
		if _, err := NewWriter(f, tc.format, tc.sampleRate, tc.channels); err == nil {
			t.Errorf("%s: want error, got nil", tc.name)
		}
		f.Close()
	}
	if _, err := NewWriter(nil, PCM24, 48000, 2); err == nil {
		t.Error("nil destination: want error, got nil")
	}
}

// TestWriterMonoFormats checks 16-bit PCM, 24-bit PCM, and 32-bit float
// mono headers plus decoded content.
func TestWriterMonoFormats(t *testing.T) {
	const sr, frames = 48000, 64
	mono := sineFrames(frames, 440, 0.5, sr)
	for _, tc := range []struct {
		name   string
		format Format
		bits   int
		header int
		mask   uint32 // extensible mask, 0 for plain PCM
	}{
		{"m16.wav", PCM16, 16, 44, 0},
		{"m24.wav", PCM24, 24, 44, 0},
		{"m32f.wav", Float32, 32, 68, 0x4},
	} {
		var f *os.File
		var w *Writer
		var err error
		if tc.format == Float32 {
			f, w = writeTemp(t, tc.name, tc.format, sr, 1)
		} else {
			f, w = writeTempBits(t, tc.name, tc.format, tc.bits, sr, 1)
		}
		if _, err = w.WriteFrames(mono); err != nil {
			t.Fatal(err)
		}
		if err = w.Close(); err != nil {
			t.Fatal(err)
		}
		path := f.Name()
		if err = f.Close(); err != nil {
			t.Fatal(err)
		}
		raw := readRaw(t, path)
		checkRIFF(t, raw)
		dataOff := 36
		if tc.mask != 0 {
			dataOff = checkExtensible(t, raw, 3, 1, sr, tc.bits, tc.mask)
		} else {
			dataOff = checkFmt16(t, raw, 1, 1, sr, tc.bits)
		}
		bytesPer := tc.bits / 8
		if got, want := checkData(t, raw, dataOff), uint32(frames*bytesPer); got != want {
			t.Fatalf("%s: data size = %d, want %d", tc.name, got, want)
		}
		if want := tc.header + frames*bytesPer; len(raw) != want {
			t.Fatalf("%s: file size = %d, want %d", tc.name, len(raw), want)
		}
		info, back, err := DecodeFile(path)
		if err != nil {
			t.Fatal(err)
		}
		if info.Channels != 1 || info.Frames != frames || info.BitsPerSample != tc.bits {
			t.Fatalf("%s: info = %+v", tc.name, info)
		}
		tol := 1.0 / math.Pow(2, float64(tc.bits-1))
		if tc.bits == 32 && tc.format == Float32 {
			tol = 1e-7
		} else if tc.bits == 32 {
			tol = 1e-7 // integer quantum below float precision
		}
		for i := 0; i < frames; i++ {
			if d := math.Abs(float64(back[i] - mono[i])); d > tol {
				t.Fatalf("%s: frame %d differs by %v", tc.name, i, d)
			}
		}
	}
}

// TestWriterMultichannel checks that layouts above stereo use
// WAVE_FORMAT_EXTENSIBLE with the quad speaker mask.
func TestWriterMultichannel(t *testing.T) {
	const sr, ch, frames = 48000, 4, 32
	f, w := writeTempBits(t, "quad.wav", PCM24, 24, sr, ch)
	chs := make([][]float32, ch)
	for c := range chs {
		chs[c] = sineFrames(frames, 440+float64(c)*110, 0.4, sr)
	}
	if n, err := w.WriteFrames(chs...); err != nil || n != frames {
		t.Fatalf("WriteFrames = %d, %v", n, err)
	}
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}
	path := f.Name()
	if err := f.Close(); err != nil {
		t.Fatal(err)
	}
	raw := readRaw(t, path)
	checkRIFF(t, raw)
	off := checkExtensible(t, raw, 1, ch, sr, 24, 0x33)
	if got, want := checkData(t, raw, off), uint32(frames*ch*3); got != want {
		t.Fatalf("data size = %d, want %d", got, want)
	}
	info, back, err := DecodeFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if info.Channels != ch || info.Frames != frames || info.ChannelMask != 0x33 {
		t.Fatalf("decoded info = %+v", info)
	}
	const lsb = 1.0 / 8388607
	for i := 0; i < frames; i++ {
		for c := 0; c < ch; c++ {
			if d := math.Abs(float64(back[i*ch+c] - chs[c][i])); d > lsb {
				t.Fatalf("frame %d ch %d differs by %v", i, c, d)
			}
		}
	}
}

// TestWriterSamplerReaderFloat writes a 32-bit float extensible file and
// reads it back through DecodeFile — the same reader the sampler uses to
// load samples — verifying normalized format info and sample accuracy.
func TestWriterSamplerReaderFloat(t *testing.T) {
	const sr, frames = 48000, 128
	f, w := writeTemp(t, "samplerfloat.wav", Float32, sr, 2)
	left := sineFrames(frames, 440, 0.8, sr)
	right := sineFrames(frames, 550, 0.6, sr)
	if _, err := w.WriteFrames(left, right); err != nil {
		t.Fatal(err)
	}
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}
	path := f.Name()
	if err := f.Close(); err != nil {
		t.Fatal(err)
	}
	info, back, err := DecodeFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if info.FormatTag != Float32 || info.BitsPerSample != 32 || info.Channels != 2 || info.Frames != frames {
		t.Fatalf("normalized info = %+v", info)
	}
	for i := 0; i < frames; i++ {
		if d := math.Abs(float64(back[2*i] - left[i])); d > 1e-7 {
			t.Fatalf("L frame %d differs by %v", i, d)
		}
		if d := math.Abs(float64(back[2*i+1] - right[i])); d > 1e-7 {
			t.Fatalf("R frame %d differs by %v", i, d)
		}
	}
}

// TestWriterSampleRates checks the rate field at 44100, 48000, and
// 96000 Hz for both encodings.
func TestWriterSampleRates(t *testing.T) {
	buf := sineFrames(32, 440, 0.5, 48000)
	for _, sr := range []int{44100, 48000, 96000} {
		for _, tc := range []struct {
			name   string
			format Format
			bits   int
		}{
			{"rate16.wav", PCM16, 16},
			{"rate24.wav", PCM24, 24},
			{"rate32i.wav", PCM32, 32},
			{"rate32f.wav", Float32, 32},
		} {
			f, w := writeTempBits(t, tc.name, tc.format, tc.bits, sr, 2)
			if _, err := w.WriteFrames(buf, buf); err != nil {
				t.Fatal(err)
			}
			if err := w.Close(); err != nil {
				t.Fatal(err)
			}
			path := f.Name()
			if err := f.Close(); err != nil {
				t.Fatal(err)
			}
			raw := readRaw(t, path)
			if got := binary.LittleEndian.Uint32(raw[24:28]); int(got) != sr {
				t.Fatalf("%s @ %d: header rate = %d", tc.name, sr, got)
			}
			info, _, err := DecodeFile(path)
			if err != nil {
				t.Fatal(err)
			}
			if info.SampleRate != sr {
				t.Fatalf("%s @ %d: decoded rate = %d", tc.name, sr, info.SampleRate)
			}
		}
	}
}
