package dsp

import (
	"math"
	"math/bits"
	"strings"
)

// Reverb is a high-density algorithmic reverb for orchestral spaces: an
// 8-line Feedback Delay Network (FDN) whose delays are the canonical
// decorrelated comb lengths at 48 kHz, each with a 1-pole HF air-damping
// filter inside the feedback loop, cross-coupled by an orthonormal
// Hadamard matrix, and finished by four Schroeder allpass diffusers per
// channel. The matrix mix is energy-preserving, so the loop gain alone
// sets the decay (RT60 ≈ 3·Tloop / −log10(g)) and no mode can ring
// metallically. Right-channel lines are offset by a prime (+23) for
// stereo width. All buffers are pre-allocated at maximum room stretch;
// processing and parameter changes perform no allocations.
type Reverb struct {
	sampleRate float64
	params     ReverbParams

	// FDN core: lines 0-3 are the left bank, 4-7 the right bank.
	lines    [8]fdnLine
	matrix   [8][8]float32 // orthonormal Hadamard, built once
	feedback float32       // loop gain derived from RoomSize
	inGain   float32
	outScale float32

	// Output diffusion: four Schroeder allpasses per channel.
	allL [4]reverbAllpass
	allR [4]reverbAllpass

	// Pre-delay lines (seconds of air before the network).
	preL, preR fdnLine

	widthCoef float32
}

// fdnBaseLengths are the delay lengths in samples at 48 kHz. The set is
// pairwise decorrelated; the +23 prime offset on the right bank keeps
// left and right reflections from ever coinciding.
var fdnBaseLengths = [8]int{1557, 1617, 1491, 1422, 1277, 1356, 1188, 1116}

// allpassBaseLengths are the diffuser delays at 48 kHz.
var allpassBaseLengths = [4]int{605, 480, 371, 245}

const (
	fdnStereoSpread    = 23
	allpassGain        = 0.5 // Schroeder allpass gain g ≈ 0.5
	maxPredelaySeconds = 0.1
	reverbInGain       = 0.30
	reverbOutScale     = 0.45
	maxRoomStretch     = 1.15 // stretch at RoomSize 1, sizes the allocations
)

// ReverbParams configures the acoustic space.
type ReverbParams struct {
	RoomSize float32 // 0..1: decay length and delay-line stretch
	Damping  float32 // 0..1: HF air absorption inside the feedback loop
	Wet      float32 // wet mix 0..1 (ProcessFrame/ProcessStereo)
	Dry      float32 // dry mix 0..1 (ProcessFrame/ProcessStereo)
	Width    float32 // stereo width 0..1
	PreDelay float32 // seconds of air before the network, 0..0.1
}

// PresetParams returns one of the built-in spaces: cathedral, hall,
// chapel, room, plate.
func PresetParams(name string) (ReverbParams, bool) {
	switch strings.ToLower(strings.TrimSpace(name)) {
	case "cathedral":
		return ReverbParams{RoomSize: 0.95, Damping: 0.22, Wet: 0.45, Dry: 0.55, Width: 0.95, PreDelay: 0.06}, true
	case "hall":
		return ReverbParams{RoomSize: 0.85, Damping: 0.30, Wet: 0.40, Dry: 0.60, Width: 0.90, PreDelay: 0.035}, true
	case "chapel":
		return ReverbParams{RoomSize: 0.78, Damping: 0.26, Wet: 0.40, Dry: 0.60, Width: 0.85, PreDelay: 0.025}, true
	case "room":
		return ReverbParams{RoomSize: 0.55, Damping: 0.40, Wet: 0.35, Dry: 0.65, Width: 0.75, PreDelay: 0.008}, true
	case "plate":
		return ReverbParams{RoomSize: 0.72, Damping: 0.55, Wet: 0.45, Dry: 0.55, Width: 1.0, PreDelay: 0.003}, true
	}
	return ReverbParams{}, false
}

// fdnLine is one power-of-two delay line whose read head sits length
// frames behind the write head.
type fdnLine struct {
	buf    []float32
	mask   int
	idx    int
	length int
	dampY  float32 // 1-pole air-damping state (FDN lines only)
}

// newFDNLine allocates a line holding up to maxLen frames.
func newFDNLine(maxLen int) fdnLine {
	if maxLen < 1 {
		maxLen = 1
	}
	capacity := 1
	for capacity <= maxLen {
		capacity <<= 1
	}
	return fdnLine{buf: make([]float32, capacity), mask: capacity - 1}
}

// read returns the sample from length frames ago.
func (l *fdnLine) read() float32 {
	return l.buf[(l.idx-l.length)&l.mask]
}

// write stores the current sample.
func (l *fdnLine) write(v float32) {
	l.buf[l.idx] = v
	l.idx = (l.idx + 1) & l.mask
}

// reset clears the line.
func (l *fdnLine) reset() {
	clear(l.buf)
	l.idx = 0
	l.dampY = 0
}

// reverbAllpass is a Schroeder allpass diffuser:
//
//	y[n] = -g·x[n] + x[n-D] + g·y[n-D]
//
// implemented with a single buffer through w[n] = x[n] + g·y[n]:
//
//	y = w[n-D] - g·x,   w[n] = (1-g²)·x + g·w[n-D]
//
// It smears transients into dense early reflections without coloring
// the magnitude response.
type reverbAllpass struct {
	line  fdnLine
	gain  float32
	gain2 float32 // 1 - gain²
}

// process advances the allpass by one sample.
func (a *reverbAllpass) process(x float32) float32 {
	w := a.line.read()
	y := w - a.gain*x
	a.line.write(a.gain2*x + a.gain*w)
	return y
}

// NewReverb allocates the network for sampleRate Hz and applies params.
// Buffers are sized for the largest room so SetParams never reallocates.
func NewReverb(sampleRate float64, params ReverbParams) *Reverb {
	if sampleRate <= 0 {
		sampleRate = 48000
	}
	rateScale := sampleRate / 48000
	r := &Reverb{
		sampleRate: sampleRate,
		inGain:     reverbInGain,
		outScale:   reverbOutScale,
	}
	spread := int(math.Ceil(float64(fdnStereoSpread) * rateScale))
	for i, base := range fdnBaseLengths {
		maxLen := int(math.Ceil(float64(base)*maxRoomStretch*rateScale)) + spread
		r.lines[i] = newFDNLine(maxLen)
	}
	for i, base := range allpassBaseLengths {
		r.allL[i] = reverbAllpass{
			line:  newFDNLine(int(math.Ceil(float64(base) * rateScale))),
			gain:  allpassGain,
			gain2: 1 - allpassGain*allpassGain,
		}
		r.allR[i] = reverbAllpass{
			line:  newFDNLine(int(math.Ceil(float64(base)*rateScale)) + spread),
			gain:  allpassGain,
			gain2: 1 - allpassGain*allpassGain,
		}
	}
	preMax := int(math.Ceil(maxPredelaySeconds * sampleRate))
	r.preL = newFDNLine(preMax)
	r.preR = newFDNLine(preMax)

	// Orthonormal Hadamard matrix H/√8 with H[i][j] = (-1)^popcount(i&j):
	// orthogonal, so feedback mixing preserves energy and spreads every
	// mode across all lines (high echo density, no periodic flutter).
	const invSqrt8 = 0.35355339059327376
	for i := 0; i < 8; i++ {
		for j := 0; j < 8; j++ {
			if bits.OnesCount(uint(i&j))%2 == 1 {
				r.matrix[i][j] = -invSqrt8
			} else {
				r.matrix[i][j] = invSqrt8
			}
		}
	}

	r.SetParams(params)
	return r
}

// SetParams retunes the space. Delay lengths change in place (buffers
// were sized for the largest room), so this never allocates; retuning
// mid-tail may click, call Reset for a clean restart.
func (r *Reverb) SetParams(p ReverbParams) {
	p.RoomSize = ClampF32(p.RoomSize, 0, 1)
	p.Damping = ClampF32(p.Damping, 0, 0.95)
	p.Wet = ClampF32(p.Wet, 0, 1)
	p.Dry = ClampF32(p.Dry, 0, 1)
	p.Width = ClampF32(p.Width, 0, 1)
	p.PreDelay = ClampF32(p.PreDelay, 0, maxPredelaySeconds)
	r.params = p

	rateScale := r.sampleRate / 48000
	stretch := float64(0.85 + 0.3*p.RoomSize)
	spread := int(math.Round(float64(fdnStereoSpread) * rateScale))
	for i, base := range fdnBaseLengths {
		n := int(math.Round(float64(base) * stretch * rateScale))
		if n < 1 {
			n = 1
		}
		if i >= 4 {
			n += spread // right bank prime offset for stereo width
		}
		r.lines[i].length = n
	}
	for i, base := range allpassBaseLengths {
		n := int(math.Round(float64(base) * rateScale))
		if n < 1 {
			n = 1
		}
		r.allL[i].line.length = n
		r.allR[i].line.length = n + spread
	}
	preLen := int(math.Round(float64(p.PreDelay) * r.sampleRate))
	r.preL.length = preLen
	r.preR.length = preLen

	// Loop gain sets RT60 (Hadamard is norm-preserving); clamp below 1
	// so the network is unconditionally stable.
	r.feedback = ClampF32(0.55+0.42*p.RoomSize, 0, 0.97)
	r.widthCoef = p.Width
}

// Params reports the current configuration.
func (r *Reverb) Params() ReverbParams { return r.params }

// RT60Seconds estimates the reverberation time from the loop gain and
// mean delay length: RT60 ≈ 3·Tloop / −log10(feedback).
func (r *Reverb) RT60Seconds() float64 {
	if r.feedback <= 0 || r.feedback >= 1 {
		return 0
	}
	mean := 0
	for i := range r.lines {
		mean += r.lines[i].length
	}
	tLoop := float64(mean) / 8 / r.sampleRate
	return 3 * tLoop / (-math.Log10(float64(r.feedback)))
}

// Reset clears every delay line and damping state.
func (r *Reverb) Reset() {
	for i := range r.lines {
		r.lines[i].reset()
	}
	for i := range r.allL {
		r.allL[i].line.reset()
		r.allR[i].line.reset()
	}
	r.preL.reset()
	r.preR.reset()
}

// ProcessFrameWet runs one stereo frame through the network and returns
// the wet signal only (send/return style, as used by the mixer).
func (r *Reverb) ProcessFrameWet(inL, inR float32) (float32, float32) {
	// Pre-delay: air between the source and the room.
	xL, xR := inL, inR
	if r.preL.length > 0 {
		xL = r.preL.read()
		r.preL.write(inL)
		xR = r.preR.read()
		r.preR.write(inR)
	}

	// Read the eight line outputs and run the air-damping one-pole
	// inside the feedback path:  d = y·(1-damp) + d_prev·damp.
	var y, d [8]float32
	damp := r.dampCoef()
	for i := range r.lines {
		y[i] = r.lines[i].read()
		ln := &r.lines[i]
		ln.dampY = y[i]*(1-damp) + ln.dampY*damp
		d[i] = ln.dampY
	}

	// FDN feedback: orthogonal Hadamard mix scaled by the loop gain,
	// plus fresh input injected into each channel's bank.
	for i := range r.lines {
		mix := float32(0)
		row := &r.matrix[i]
		for j := 0; j < 8; j++ {
			mix += row[j] * d[j]
		}
		x := xL
		if i >= 4 {
			x = xR
		}
		r.lines[i].write(mix*r.feedback + x*r.inGain)
	}

	// Bank sums are the raw wet taps.
	wetL := (y[0] + y[1] + y[2] + y[3]) * r.outScale
	wetR := (y[4] + y[5] + y[6] + y[7]) * r.outScale

	// Diffusion: series allpasses smear the taps into a dense tail.
	for i := range r.allL {
		wetL = r.allL[i].process(wetL)
	}
	for i := range r.allR {
		wetR = r.allR[i].process(wetR)
	}

	// Stereo width: 1 keeps the banks apart, 0 collapses to mono.
	mono := (wetL + wetR) * 0.5
	w := r.widthCoef
	return mono + (wetL-mono)*w, mono + (wetR-mono)*w
}

// dampCoef returns the air-damping coefficient from params.
func (r *Reverb) dampCoef() float32 { return r.params.Damping }

// ProcessFrame returns one frame of the full wet/dry mix per params.
func (r *Reverb) ProcessFrame(inL, inR float32) (float32, float32) {
	wL, wR := r.ProcessFrameWet(inL, inR)
	return inL*r.params.Dry + wL*r.params.Wet,
		inR*r.params.Dry + wR*r.params.Wet
}

// ProcessStereo processes a planar stereo buffer in place with the
// wet/dry mix.
func (r *Reverb) ProcessStereo(buf *StereoBuffer) {
	if buf == nil {
		return
	}
	left, right := buf.Left, buf.Right
	n := len(left)
	if len(right) < n {
		n = len(right)
	}
	for i := 0; i < n; i++ {
		left[i], right[i] = r.ProcessFrame(left[i], right[i])
	}
}
