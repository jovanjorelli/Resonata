// Package encoder maps CLI output selections onto WAV encodings and
// shapes engine stereo audio into the configured channel layout. It
// owns the supported format matrix in one place so the CLI and any
// future front ends resolve identical outputs.
package encoder

import (
	"fmt"
	"io"
	"strings"

	"resonata/pkg/wav"
)

// Spec is a fully resolved output encoding: sample rate, channel count,
// WAV format tag, and container bit depth.
type Spec struct {
	Format     wav.Format
	Bits       int
	Channels   int
	SampleRate int
}

// Resolve validates a bit-depth name, channel count, and sample rate
// into a Spec. Depths are "16", "24", "32" (integer PCM) and "32f"
// (32-bit IEEE float); rates are 44100, 48000, or 96000; channels are 1
// (mono) or 2 (stereo). An empty depth selects 24-bit PCM and a
// non-positive channel count selects stereo.
func Resolve(bitDepth string, channels, sampleRate int) (Spec, error) {
	var s Spec
	switch strings.ToLower(strings.TrimSpace(bitDepth)) {
	case "16":
		s.Format, s.Bits = wav.PCM16, 16
	case "24", "":
		s.Format, s.Bits = wav.PCM24, 24
	case "32":
		s.Format, s.Bits = wav.PCM32, 32
	case "32f", "float", "float32":
		s.Format, s.Bits = wav.Float32, 32
	default:
		return Spec{}, fmt.Errorf("encoder: unsupported bit depth %q (want 16, 24, 32, or 32f)", bitDepth)
	}
	switch channels {
	case 0:
		s.Channels = 2
	case 1, 2:
		s.Channels = channels
	default:
		return Spec{}, fmt.Errorf("encoder: unsupported channel count %d (want 1 or 2)", channels)
	}
	switch sampleRate {
	case 44100, 48000, 96000:
		s.SampleRate = sampleRate
	default:
		return Spec{}, fmt.Errorf("encoder: unsupported sample rate %d (want 44100, 48000, or 96000)", sampleRate)
	}
	return s, nil
}

// NewWriter opens a WAV writer for the spec on w.
func (s Spec) NewWriter(w io.WriteSeeker) (*wav.Writer, error) {
	return wav.NewWriterBits(w, s.Format, s.Bits, s.SampleRate, s.Channels)
}

// DownmixMono averages planar stereo into dst, returning the frames
// written. dst must hold at least n frames; only the first n entries of
// each channel are read.
func DownmixMono(dst, left, right []float32, n int) int {
	if n > len(dst) {
		n = len(dst)
	}
	if n > len(left) {
		n = len(left)
	}
	if n > len(right) {
		n = len(right)
	}
	for i := 0; i < n; i++ {
		dst[i] = (left[i] + right[i]) * 0.5
	}
	return n
}
