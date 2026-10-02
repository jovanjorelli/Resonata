package engine

import (
	"os"

	"resonata/pkg/score"
	"resonata/pkg/wav"
)

// OfflineRenderer renders a score to a WAV file block by block. Drive it
// from a worker goroutine:
//
//	for !r.Done() { r.ProcessBlock() }
//	if err := r.Err(); err != nil { ... }
//	r.Close()
//
// Progress reports the completed fraction for CLI progress output.
// All scratch buffers are allocated up front; the render loop performs
// no allocations.
type OfflineRenderer struct {
	eng   *Engine
	file  *os.File
	w     *wav.Writer
	sink  func(master []float32)
	left  []float32 // de-interleaving scratch
	right []float32
	total int // frames to render
	done  int
	peak  float32
	err   error
}

// NewOfflineRenderer builds the engine for s and opens path for 24-bit
// stereo output at sampleRate Hz.
func NewOfflineRenderer(s *score.Score, path string, sampleRate float64) (*OfflineRenderer, error) {
	eng, err := New(s, sampleRate, DefaultBlockSize)
	if err != nil {
		return nil, err
	}
	f, err := os.Create(path)
	if err != nil {
		return nil, err
	}
	w, err := wav.NewWriter(f, wav.PCM24, int(sampleRate), 2)
	if err != nil {
		f.Close()
		return nil, err
	}
	r := &OfflineRenderer{
		eng:   eng,
		file:  f,
		w:     w,
		left:  make([]float32, eng.BlockSize()),
		right: make([]float32, eng.BlockSize()),
		total: eng.TotalFrames(),
	}
	r.sink = r.writeChunk
	return r, nil
}

// Engine exposes the underlying engine (score, duration, meters).
func (r *OfflineRenderer) Engine() *Engine { return r.eng }

// Done reports whether rendering finished, successfully or not.
func (r *OfflineRenderer) Done() bool { return r.err != nil || r.done >= r.total }

// Progress is the completed fraction in [0, 1].
func (r *OfflineRenderer) Progress() float32 {
	if r.total <= 0 {
		return 1
	}
	p := float32(r.done) / float32(r.total)
	if p > 1 {
		p = 1
	}
	return p
}

// Err returns the first failure recorded during rendering, if any.
func (r *OfflineRenderer) Err() error { return r.err }

// Peak is the largest master magnitude rendered so far.
func (r *OfflineRenderer) Peak() float32 { return r.peak }

// Frames is the total number of frames the render will produce.
func (r *OfflineRenderer) Frames() int { return r.total }

// ProcessBlock renders the next block to the file. Failures are recorded
// and surfaced by Err; Done becomes true.
func (r *OfflineRenderer) ProcessBlock() {
	if r.Done() {
		return
	}
	n := min(r.eng.BlockSize(), r.total-r.done)
	r.eng.ProcessFrames(n, r.sink)
	r.done += n
}

// writeChunk de-interleaves one rendered chunk into the WAV writer.
func (r *OfflineRenderer) writeChunk(master []float32) {
	if r.err != nil {
		return
	}
	frames := len(master) / 2
	for i := 0; i < frames; i++ {
		l, rt := master[2*i], master[2*i+1]
		r.left[i], r.right[i] = l, rt
		if a := abs32(l); a > r.peak {
			r.peak = a
		}
		if a := abs32(rt); a > r.peak {
			r.peak = a
		}
	}
	if _, err := r.w.WriteFrames(r.left[:frames], r.right[:frames]); err != nil {
		r.err = err
	}
}

// Close finalizes the WAV chunk sizes and closes the file. Call after
// Done; it reports the first of any render, finalize, or close errors.
func (r *OfflineRenderer) Close() error {
	werr := r.w.Close()
	ferr := r.file.Close()
	switch {
	case r.err != nil:
		return r.err
	case werr != nil:
		return werr
	}
	return ferr
}
