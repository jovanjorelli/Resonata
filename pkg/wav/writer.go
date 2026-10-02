// Package wav writes RIFF WAVE files with integer PCM (16/24/32 bit)
// or 32-bit IEEE float sample encoding, using plain format tags for
// mono/stereo and WAVE_FORMAT_EXTENSIBLE for float and multichannel
// layouts.
package wav

import (
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"math"
)

// Format selects the sample encoding of a WAV file. PCM16, PCM24, and
// PCM32 share wFormatTag 1 (integer PCM) with different container
// widths; Float32 is wFormatTag 3; Extensible (0xFFFE) wraps either
// encoding with speaker positions for float and multichannel files.
type Format uint16

const (
	PCM16      Format = 1 // 16-bit little-endian signed integer
	PCM24      Format = 1 // 24-bit little-endian signed integer
	PCM32      Format = 1 // 32-bit little-endian signed integer
	Float32    Format = 3 // 32-bit IEEE float
	Extensible Format = 0xFFFE
)

// Integer full scales: float [-1, 1] maps onto the symmetric range.
const (
	int16Max = 32767      // 2^15 - 1
	int24Max = 8388607    // 2^23 - 1
	int32Max = 2147483647 // 2^31 - 1
)

// Speaker positions for the extensible channel mask.
const (
	speakerFrontLeft   = 0x1
	speakerFrontRight  = 0x2
	speakerFrontCenter = 0x4
	speakerBackLeft    = 0x10
	speakerBackRight   = 0x20
)

// guidTail is the shared KSDATAFORMAT_SUBTYPE tail; the head DWORD
// carries the wrapped format tag (1 for PCM, 3 for float).
var guidTail = []byte{0x00, 0x00, 0x10, 0x00, 0x80, 0x00, 0x00, 0xAA, 0x00, 0x38, 0x9B, 0x71}

// channelMask returns the speaker bitmask for n channels: mono maps to
// front-center, stereo to front-left/right, larger layouts to the
// standard surround masks with a low-bits fallback.
func channelMask(channels int) uint32 {
	switch channels {
	case 1:
		return speakerFrontCenter
	case 2:
		return speakerFrontLeft | speakerFrontRight
	case 3:
		return 0x7 // FL FR FC
	case 4:
		return 0x33 // FL FR BL BR (quad)
	case 5:
		return 0x37 // FL FR FC BL BR
	case 6:
		return 0x3F // 5.1: FL FR FC LFE BL BR
	}
	if channels >= 32 {
		return 0xFFFFFFFF
	}
	return uint32(1)<<uint(channels) - 1
}

// scratchFrames is the initial interleaving scratch size in frames.
const scratchFrames = 4096

// Writer streams WAVE data to an io.WriteSeeker (e.g. *os.File). The
// header is written up front with placeholder sizes; Close seeks back to
// patch the RIFF and data chunk sizes (plus the extensible frame count
// when present), so the destination must be seekable. All per-sample
// work reuses a pre-allocated scratch buffer.
type Writer struct {
	w              io.WriteSeeker
	format         Format // resolved storage tag: 1 or 0xFFFE
	subFormat      Format // wrapped encoding for extensible files
	sampleRate     int
	channels       int
	bitsPerSample  int
	bytesPerSample int
	blockAlign     int
	extensible     bool
	frames         uint64 // frames per channel written so far
	dataSizeOff    int64  // file offset of the data chunk size field
	closed         bool
	scratch        []byte // interleaved staging buffer, reused and grown
}

// NewWriter emits the WAVE header for the given layout and returns a
// writer streaming float samples at sampleRate Hz. PCM24 selects 24-bit
// integer PCM and Float32 selects 32-bit IEEE float; finer control needs
// NewWriterBits. Call Close to finalize the chunk sizes.
func NewWriter(w io.WriteSeeker, format Format, sampleRate, channels int) (*Writer, error) {
	switch format {
	case PCM24:
		return NewWriterBits(w, PCM24, 24, sampleRate, channels)
	case Float32:
		return NewWriterBits(w, Float32, 32, sampleRate, channels)
	default:
		return nil, fmt.Errorf("wav: ambiguous format tag %d, use NewWriterBits", uint16(format))
	}
}

// NewWriterBits emits the WAVE header for an explicit encoding: format
// PCM16/PCM24/PCM32 (tag 1) with bits 16/24/32, or Float32 with 32 bits.
// Mono and stereo PCM use the plain 16-byte fmt chunk; float encodings
// and anything above two channels use WAVE_FORMAT_EXTENSIBLE with a
// 40-byte fmt chunk carrying the channel mask and SubFormat GUID.
func NewWriterBits(w io.WriteSeeker, format Format, bits, sampleRate, channels int) (*Writer, error) {
	if w == nil {
		return nil, errors.New("wav: nil destination")
	}
	if sampleRate <= 0 || channels <= 0 {
		return nil, fmt.Errorf("wav: invalid layout: %d Hz, %d channels", sampleRate, channels)
	}
	tag := uint16(format)
	var sub Format
	switch tag {
	case 1:
		sub = PCM24
		switch bits {
		case 16, 24, 32:
		default:
			return nil, fmt.Errorf("wav: unsupported PCM bit depth %d", bits)
		}
	case 3:
		sub = Float32
		if bits != 32 {
			return nil, fmt.Errorf("wav: unsupported float bit depth %d", bits)
		}
	default:
		return nil, fmt.Errorf("wav: unsupported format tag %d", tag)
	}
	extensible := sub == Float32 || channels > 2
	stored := Format(1)
	if extensible {
		stored = Extensible
	}
	wr := &Writer{
		w:              w,
		format:         stored,
		subFormat:      sub,
		sampleRate:     sampleRate,
		channels:       channels,
		bitsPerSample:  bits,
		bytesPerSample: bits / 8,
		blockAlign:     channels * bits / 8,
		extensible:     extensible,
		scratch:        make([]byte, channels*(bits/8)*scratchFrames),
	}
	if err := wr.writeHeader(); err != nil {
		return nil, err
	}
	return wr, nil
}

// writeHeader emits the RIFF, fmt, and data chunks with placeholder
// sizes patched by Close. Layout (offsets are file positions):
//
//	"RIFF" <size> "WAVE" "fmt " <fmtLen> <fmt> "data" <size>
//
// with fmtLen 16 for plain PCM (44-byte header) and 40 for extensible
// files (68-byte header: standard fields, cbSize 22, valid bits,
// channel mask, 16-byte SubFormat GUID).
func (wr *Writer) writeHeader() error {
	fmtLen := uint32(16)
	headerLen := 44
	if wr.extensible {
		fmtLen = 40
		headerLen = 68
	}
	b := make([]byte, headerLen)
	copy(b[0:4], "RIFF")
	binary.LittleEndian.PutUint32(b[4:8], 0) // RIFF size, patched in Close
	copy(b[8:12], "WAVE")
	copy(b[12:16], "fmt ")
	binary.LittleEndian.PutUint32(b[16:20], fmtLen)
	binary.LittleEndian.PutUint16(b[20:22], uint16(wr.format))
	binary.LittleEndian.PutUint16(b[22:24], uint16(wr.channels))
	binary.LittleEndian.PutUint32(b[24:28], uint32(wr.sampleRate))
	binary.LittleEndian.PutUint32(b[28:32], uint32(wr.sampleRate*wr.blockAlign))
	binary.LittleEndian.PutUint16(b[32:34], uint16(wr.blockAlign))
	binary.LittleEndian.PutUint16(b[34:36], uint16(wr.bitsPerSample))
	end := 36
	if wr.extensible {
		binary.LittleEndian.PutUint16(b[36:38], 22) // cbSize
		binary.LittleEndian.PutUint16(b[38:40], uint16(wr.bitsPerSample))
		binary.LittleEndian.PutUint32(b[40:44], channelMask(wr.channels))
		binary.LittleEndian.PutUint32(b[44:48], uint32(wr.subFormat))
		copy(b[48:60], guidTail)
		end = 60
	}
	copy(b[end:end+4], "data")
	wr.dataSizeOff = int64(end + 4)
	binary.LittleEndian.PutUint32(b[end+4:end+8], 0) // data size, patched in Close
	_, err := wr.w.Write(b)
	return err
}

// WriteFrames writes one block of audio, one float32 slice per channel.
// The longest slice defines the frame count and missing samples are
// written as silence; passing more slices than configured channels is an
// error. It returns the number of frames written.
func (wr *Writer) WriteFrames(chans ...[]float32) (int, error) {
	if wr.closed {
		return 0, errors.New("wav: write after Close")
	}
	if len(chans) > wr.channels {
		return 0, fmt.Errorf("wav: got %d channel slices, want at most %d", len(chans), wr.channels)
	}
	n := 0
	for _, c := range chans {
		if len(c) > n {
			n = len(c)
		}
	}
	if n == 0 {
		return 0, nil
	}
	if need := n * wr.blockAlign; len(wr.scratch) < need {
		wr.scratch = make([]byte, need) // one-off grow for oversized blocks
	}
	switch {
	case wr.subFormat == Float32:
		wr.encodeFloat32(chans, n)
	case wr.bitsPerSample == 16:
		wr.encodePCM16(chans, n)
	case wr.bitsPerSample == 32:
		wr.encodePCM32(chans, n)
	default:
		wr.encodePCM24(chans, n)
	}
	if _, err := wr.w.Write(wr.scratch[:n*wr.blockAlign]); err != nil {
		return 0, err
	}
	wr.frames += uint64(n)
	return n, nil
}

// encodePCM16 interleaves chans into scratch as clamped 16-bit integers.
func (wr *Writer) encodePCM16(chans [][]float32, n int) {
	for i := 0; i < n; i++ {
		for ch := 0; ch < wr.channels; ch++ {
			var s float32
			if ch < len(chans) && i < len(chans[ch]) {
				s = chans[ch][i]
			}
			off := (i*wr.channels + ch) * 2
			binary.LittleEndian.PutUint16(wr.scratch[off:off+2], uint16(floatToInt16(s)))
		}
	}
}

// encodePCM24 interleaves chans into scratch as clamped 24-bit integers.
func (wr *Writer) encodePCM24(chans [][]float32, n int) {
	for i := 0; i < n; i++ {
		for ch := 0; ch < wr.channels; ch++ {
			var s float32
			if ch < len(chans) && i < len(chans[ch]) {
				s = chans[ch][i]
			}
			v := floatToInt24(s)
			off := (i*wr.channels + ch) * 3
			wr.scratch[off] = byte(v)
			wr.scratch[off+1] = byte(v >> 8)
			wr.scratch[off+2] = byte(v >> 16)
		}
	}
}

// encodePCM32 interleaves chans into scratch as clamped 32-bit integers.
func (wr *Writer) encodePCM32(chans [][]float32, n int) {
	for i := 0; i < n; i++ {
		for ch := 0; ch < wr.channels; ch++ {
			var s float32
			if ch < len(chans) && i < len(chans[ch]) {
				s = chans[ch][i]
			}
			off := (i*wr.channels + ch) * 4
			binary.LittleEndian.PutUint32(wr.scratch[off:off+4], uint32(floatToInt32(s)))
		}
	}
}

// encodeFloat32 interleaves chans into scratch as raw IEEE floats.
func (wr *Writer) encodeFloat32(chans [][]float32, n int) {
	for i := 0; i < n; i++ {
		for ch := 0; ch < wr.channels; ch++ {
			var s float32
			if ch < len(chans) && i < len(chans[ch]) {
				s = chans[ch][i]
			}
			off := (i*wr.channels + ch) * 4
			binary.LittleEndian.PutUint32(wr.scratch[off:off+4], math.Float32bits(s))
		}
	}
}

// floatToInt16 maps a float32 sample in [-1, 1] onto the int16 range
// [-32767, 32767], clamping out-of-range and NaN input.
func floatToInt16(f float32) int16 {
	v := float64(f) * int16Max
	switch {
	case math.IsNaN(v):
		return 0
	case v >= int16Max:
		return int16Max
	case v <= -int16Max:
		return -int16Max
	}
	return int16(math.Round(v))
}

// floatToInt24 maps a float32 sample in [-1, 1] onto the int24 range
// [-8388607, 8388607], clamping out-of-range and NaN input.
func floatToInt24(f float32) int32 {
	v := float64(f) * int24Max
	switch {
	case math.IsNaN(v):
		return 0
	case v >= int24Max:
		return int24Max
	case v <= -int24Max:
		return -int24Max
	}
	return int32(math.Round(v))
}

// floatToInt32 maps a float32 sample in [-1, 1] onto the int32 range
// [-2147483647, 2147483647], clamping out-of-range and NaN input.
func floatToInt32(f float32) int32 {
	v := float64(f) * int32Max
	switch {
	case math.IsNaN(v):
		return 0
	case v >= int32Max:
		return int32Max
	case v <= -int32Max:
		return -int32Max
	}
	return int32(math.Round(v))
}

// SampleRate reports the configured frame rate in Hz.
func (wr *Writer) SampleRate() int { return wr.sampleRate }

// Channels reports the configured channel count.
func (wr *Writer) Channels() int { return wr.channels }

// Frames reports the frames per channel written so far.
func (wr *Writer) Frames() uint64 { return wr.frames }

// Close patches the RIFF and data chunk sizes in the header: RIFF size
// equals file size minus 8 (36 plus data for standard headers, 60 plus
// data for extensible ones). The underlying stream is left open and
// positioned at end of file.
func (wr *Writer) Close() error {
	if wr.closed {
		return nil
	}
	wr.closed = true
	dataSize := wr.frames * uint64(wr.blockAlign)
	// RIFF size = total file size - 8 = (dataSizeOff + 4 + dataSize) - 8.
	riffSize := uint64(wr.dataSizeOff) - 4 + dataSize
	if riffSize > math.MaxUint32 {
		return errors.New("wav: stream exceeds the 4 GiB RIFF size limit")
	}
	var patch [4]byte
	if err := wr.patchAt(4, uint32(riffSize), patch[:]); err != nil {
		return err
	}
	if err := wr.patchAt(wr.dataSizeOff, uint32(dataSize), patch[:]); err != nil {
		return err
	}
	_, err := wr.w.Seek(0, io.SeekEnd)
	return err
}

// patchAt writes one little-endian uint32 at the given file offset.
func (wr *Writer) patchAt(off int64, v uint32, scratch []byte) error {
	binary.LittleEndian.PutUint32(scratch, v)
	if _, err := wr.w.Seek(off, io.SeekStart); err != nil {
		return err
	}
	_, err := wr.w.Write(scratch)
	return err
}
