// Package dsp provides zero-allocation audio DSP primitives for Resonata.
package dsp

// StereoBuffer holds two pre-allocated float32 channel slices. Capacity is
// fixed at construction; buffers are reused across blocks via the
// buf = buf[:0] pattern (see Reset), so rendering never reallocates.
type StereoBuffer struct {
	Left     []float32
	Right    []float32
	capacity int
}

// Buffer is an alias of StereoBuffer so instrument APIs can use the
// shorter dsp.Buffer name for the same pre-allocated stereo buffer.
type Buffer = StereoBuffer

// NewStereoBuffer pre-allocates a stereo buffer holding up to frames frames.
func NewStereoBuffer(frames int) *StereoBuffer {
	if frames < 0 {
		frames = 0
	}
	return &StereoBuffer{
		Left:     make([]float32, 0, frames),
		Right:    make([]float32, 0, frames),
		capacity: frames,
	}
}

// Len reports the active frame count.
func (b *StereoBuffer) Len() int { return len(b.Left) }

// Cap reports the frame capacity fixed at allocation.
func (b *StereoBuffer) Cap() int { return b.capacity }

// Reset truncates both channels with the buf = buf[:0] reuse pattern,
// keeping the underlying arrays for the next block.
func (b *StereoBuffer) Reset() {
	b.Left = b.Left[:0]
	b.Right = b.Right[:0]
}

// SetLen sets the active frame count, zero-filling newly exposed frames.
// n is clamped to [0, Cap].
func (b *StereoBuffer) SetLen(n int) {
	if n < 0 {
		n = 0
	}
	if n > b.capacity {
		n = b.capacity
	}
	if n > len(b.Left) {
		clear(b.Left[len(b.Left):n])
		clear(b.Right[len(b.Right):n])
	}
	b.Left = b.Left[:n]
	b.Right = b.Right[:n]
}

// Clear silences all active frames.
func (b *StereoBuffer) Clear() {
	clear(b.Left)
	clear(b.Right)
}

// Copy replaces the active frames of b with the leading frames of src,
// clamped to the capacity of b.
func (b *StereoBuffer) Copy(src *StereoBuffer) {
	n := src.Len()
	if n > b.capacity {
		n = b.capacity
	}
	b.SetLen(n)
	copy(b.Left, src.Left[:n])
	copy(b.Right, src.Right[:n])
}

// Mix adds src into b frame-by-frame without clipping. Frames beyond the
// shorter buffer are left untouched.
func (b *StereoBuffer) Mix(src *StereoBuffer) {
	n := b.Len()
	if src.Len() < n {
		n = src.Len()
	}
	for i := 0; i < n; i++ {
		b.Left[i] += src.Left[i]
		b.Right[i] += src.Right[i]
	}
}

// MixGain adds src scaled by gain into b, the common case when summing
// voices at a per-voice level.
func (b *StereoBuffer) MixGain(src *StereoBuffer, gain float32) {
	n := b.Len()
	if src.Len() < n {
		n = src.Len()
	}
	for i := 0; i < n; i++ {
		b.Left[i] += src.Left[i] * gain
		b.Right[i] += src.Right[i] * gain
	}
}
