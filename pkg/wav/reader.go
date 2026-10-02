package wav

import (
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"math"
	"os"
)

// Info describes a decoded WAV file. Extensible headers (tag 0xFFFE)
// normalize to their wrapped encoding: FormatTag becomes PCM24 or
// Float32 and BitsPerSample the valid bit depth, with ChannelMask
// preserving the speaker layout.
type Info struct {
	FormatTag     Format // PCM24 (tag 1) for integer PCM, Float32 (tag 3) for float
	SampleRate    int
	Channels      int
	BitsPerSample int
	ChannelMask   uint32 // speaker positions from extensible headers, 0 when absent
	Frames        int    // decoded frames per channel
	LoopStart     int    // smpl chunk loop points in frames; zero when absent
	LoopEnd       int    // exclusive end frame
	HasLoop       bool
}

// DecodeFile reads and decodes the WAV file at path.
func DecodeFile(path string) (Info, []float32, error) {
	f, err := os.Open(path)
	if err != nil {
		return Info{}, nil, err
	}
	defer f.Close()
	return Decode(f)
}

// Decode reads a RIFF WAVE stream, converting integer PCM (8/16/24/32
// bit) and IEEE float (32/64 bit) samples to float32 in [-1, 1]. The
// returned slice holds interleaved frames. Loop points are taken from the
// smpl chunk when present; unknown chunks are skipped.
func Decode(r io.ReadSeeker) (Info, []float32, error) {
	var info Info
	var hdr [12]byte
	if _, err := io.ReadFull(r, hdr[:]); err != nil {
		return info, nil, fmt.Errorf("wav: short RIFF header: %w", err)
	}
	if string(hdr[0:4]) != "RIFF" || string(hdr[8:12]) != "WAVE" {
		return info, nil, errors.New("wav: not a RIFF WAVE file")
	}

	var samples []float32
	haveFmt, haveData := false, false
	var chunkHdr [8]byte
	for {
		if _, err := io.ReadFull(r, chunkHdr[:]); err != nil {
			if err == io.EOF {
				break
			}
			return info, nil, fmt.Errorf("wav: reading chunk header: %w", err)
		}
		id := string(chunkHdr[0:4])
		size := int64(binary.LittleEndian.Uint32(chunkHdr[4:8]))
		switch id {
		case "fmt ":
			if size < 16 {
				return info, nil, errors.New("wav: fmt chunk too small")
			}
			var fb [16]byte
			if _, err := io.ReadFull(r, fb[:]); err != nil {
				return info, nil, fmt.Errorf("wav: reading fmt chunk: %w", err)
			}
			info.FormatTag = Format(binary.LittleEndian.Uint16(fb[0:2]))
			info.Channels = int(binary.LittleEndian.Uint16(fb[2:4]))
			info.SampleRate = int(binary.LittleEndian.Uint32(fb[4:8]))
			info.BitsPerSample = int(binary.LittleEndian.Uint16(fb[14:16]))
			rest := size - 16
			if info.FormatTag == Extensible {
				if err := parseExtensible(r, &info, &rest); err != nil {
					return info, nil, err
				}
			}
			if err := skip(r, rest); err != nil { // fmt extension bytes
				return info, nil, err
			}
			if err := validateFmt(info); err != nil {
				return info, nil, err
			}
			haveFmt = true
		case "data":
			if !haveFmt {
				return info, nil, errors.New("wav: data chunk before fmt")
			}
			var err error
			samples, info.Frames, err = decodeData(r, info, size)
			if err != nil {
				return info, nil, err
			}
			haveData = true
		case "smpl":
			if err := decodeSmpl(r, &info, size); err != nil {
				return info, nil, err
			}
		default:
			if err := skip(r, size); err != nil {
				return info, nil, err
			}
		}
		if size%2 == 1 { // chunks are word-aligned
			if err := skip(r, 1); err != nil {
				return info, nil, err
			}
		}
	}
	if !haveData {
		return info, nil, errors.New("wav: no data chunk")
	}
	return info, samples, nil
}

// parseExtensible resolves a WAVE_FORMAT_EXTENSIBLE fmt tail: the
// SubFormat GUID selects PCM or float, the valid bit depth becomes the
// sample width, and the channel mask records the speaker layout. info is
// normalized to the wrapped encoding so downstream code needs no
// extensible-specific paths. rest is reduced by the bytes consumed.
func parseExtensible(r io.ReadSeeker, info *Info, rest *int64) error {
	var ext [24]byte
	if *rest < 24 {
		return errors.New("wav: truncated extensible fmt chunk")
	}
	if _, err := io.ReadFull(r, ext[:]); err != nil {
		return fmt.Errorf("wav: reading extensible fmt: %w", err)
	}
	*rest -= 24
	if got := binary.LittleEndian.Uint16(ext[0:2]); got != 22 {
		return fmt.Errorf("wav: extensible cbSize = %d, want 22", got)
	}
	validBits := int(binary.LittleEndian.Uint16(ext[2:4]))
	info.ChannelMask = binary.LittleEndian.Uint32(ext[4:8])
	sub := binary.LittleEndian.Uint32(ext[8:12])
	switch sub {
	case 1:
		info.FormatTag = PCM24
	case 3:
		info.FormatTag = Float32
	default:
		return fmt.Errorf("wav: unsupported extensible subformat %d", sub)
	}
	if validBits <= 0 {
		return errors.New("wav: extensible valid bits must be positive")
	}
	info.BitsPerSample = validBits
	return nil
}

// validateFmt checks the format combination Decode can convert.
func validateFmt(info Info) error {
	switch info.FormatTag {
	case PCM24:
		switch info.BitsPerSample {
		case 8, 16, 24, 32:
		default:
			return fmt.Errorf("wav: unsupported PCM bit depth %d", info.BitsPerSample)
		}
	case Float32:
		switch info.BitsPerSample {
		case 32, 64:
		default:
			return fmt.Errorf("wav: unsupported float bit depth %d", info.BitsPerSample)
		}
	default:
		return fmt.Errorf("wav: unsupported format tag %d (want 1 or 3)", uint16(info.FormatTag))
	}
	if info.Channels <= 0 || info.SampleRate <= 0 {
		return errors.New("wav: invalid channel count or sample rate")
	}
	return nil
}

// decodeData reads the data chunk and converts it to interleaved float32.
// A truncated chunk yields the frames actually present.
func decodeData(r io.ReadSeeker, info Info, size int64) ([]float32, int, error) {
	blockAlign := info.Channels * info.BitsPerSample / 8
	if blockAlign <= 0 {
		return nil, 0, errors.New("wav: invalid block alignment")
	}
	frames := int(size / int64(blockAlign))
	raw := make([]byte, frames*blockAlign)
	n, err := io.ReadFull(r, raw)
	if err != nil && !errors.Is(err, io.ErrUnexpectedEOF) && err != io.EOF {
		return nil, 0, fmt.Errorf("wav: reading data chunk: %w", err)
	}
	frames = n / blockAlign
	raw = raw[:frames*blockAlign]
	out := make([]float32, frames*info.Channels)

	switch {
	case info.FormatTag == Float32 && info.BitsPerSample == 32:
		for i := range out {
			out[i] = math.Float32frombits(binary.LittleEndian.Uint32(raw[i*4:]))
		}
	case info.FormatTag == Float32 && info.BitsPerSample == 64:
		for i := range out {
			out[i] = float32(math.Float64frombits(binary.LittleEndian.Uint64(raw[i*8:])))
		}
	case info.FormatTag == PCM24:
		switch info.BitsPerSample {
		case 8: // 8-bit PCM is unsigned
			for i := range out {
				out[i] = (float32(raw[i]) - 128) / 128
			}
		case 16:
			for i := range out {
				out[i] = float32(int16(binary.LittleEndian.Uint16(raw[i*2:]))) / 32768
			}
		case 24:
			for i := range out {
				v := int32(raw[i*3]) | int32(raw[i*3+1])<<8 | int32(raw[i*3+2])<<16
				if v&0x800000 != 0 {
					v -= 1 << 24
				}
				out[i] = float32(v) / 8388608
			}
		case 32:
			for i := range out {
				out[i] = float32(int32(binary.LittleEndian.Uint32(raw[i*4:]))) / 2147483648
			}
		}
	default:
		return nil, 0, fmt.Errorf("wav: unsupported format tag %d with %d bits",
			uint16(info.FormatTag), info.BitsPerSample)
	}
	return out, frames, nil
}

// decodeSmpl extracts the first loop point from a smpl chunk: a 36-byte
// header with the loop count at offset 28, then 24-byte loop records with
// start/end at offsets 8/12.
func decodeSmpl(r io.ReadSeeker, info *Info, size int64) error {
	if size < 36 {
		return skip(r, size)
	}
	var hdr [36]byte
	if _, err := io.ReadFull(r, hdr[:]); err != nil {
		return fmt.Errorf("wav: reading smpl chunk: %w", err)
	}
	consumed := int64(36)
	if numLoops := binary.LittleEndian.Uint32(hdr[28:32]); numLoops > 0 && size >= consumed+24 {
		var loop [24]byte
		if _, err := io.ReadFull(r, loop[:]); err != nil {
			return fmt.Errorf("wav: reading smpl loop: %w", err)
		}
		consumed += 24
		start := binary.LittleEndian.Uint32(loop[8:12])
		end := binary.LittleEndian.Uint32(loop[12:16])
		if end > start {
			info.LoopStart, info.LoopEnd, info.HasLoop = int(start), int(end), true
		}
	}
	return skip(r, size-consumed)
}

// skip advances the stream by n bytes.
func skip(r io.ReadSeeker, n int64) error {
	if n <= 0 {
		return nil
	}
	_, err := r.Seek(n, io.SeekCurrent)
	return err
}
