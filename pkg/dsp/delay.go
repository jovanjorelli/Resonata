package dsp

import (
	"math"
	"strings"
)

// MaxDelaySeconds is the longest echo time the ring buffers can hold.
// Buffers are sized once at construction, so the audio loop never grows.
const MaxDelaySeconds = 4.0

// DefaultDelayDampingHz is the warm analog tape cutoff used when a
// configuration leaves damping unset.
const DefaultDelayDampingHz = 4000.0

// DelayMode selects the feedback topology of the stereo echo.
type DelayMode int

const (
	// DelayModeStereo feeds each channel back into itself.
	DelayModeStereo DelayMode = iota
	// DelayModePingPong cross-feeds left into right and right into
	// left so repeats bounce across the stereo image.
	DelayModePingPong
)

// Subdivision is a BPM-relative note value. A quarter note equals one
// beat, so delay time is 60/BPM * subdivision seconds.
type Subdivision float64

// Musical subdivisions for BPM-synced echo times.
const (
	SubdivisionWhole      Subdivision = 4.0
	SubdivisionHalf       Subdivision = 2.0
	SubdivisionQuarter    Subdivision = 1.0
	SubdivisionEighth     Subdivision = 0.5
	SubdivisionEighthD    Subdivision = 0.75
	SubdivisionSixteenth  Subdivision = 0.25
	SubdivisionSixteenthT Subdivision = 1.0 / 3.0
)

// ParseSubdivision maps score DSL names to subdivisions. Accepted forms
// are "1/1", "1/2", "1/4", "1/8", "1/8d", "1/16", "1/16t" with optional
// surrounding whitespace and case-insensitive dotted/triplet suffixes.
func ParseSubdivision(s string) (Subdivision, bool) {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "1/1", "whole":
		return SubdivisionWhole, true
	case "1/2", "half":
		return SubdivisionHalf, true
	case "1/4", "quarter":
		return SubdivisionQuarter, true
	case "1/8", "eighth":
		return SubdivisionEighth, true
	case "1/8d", "1/8.", "dotted-eighth", "eighthd":
		return SubdivisionEighthD, true
	case "1/16", "sixteenth":
		return SubdivisionSixteenth, true
	case "1/16t", "sixteentht", "1/16t.", "triplet":
		return SubdivisionSixteenthT, true
	}
	return 0, false
}

// DelaySamplesFor converts a BPM-synced subdivision to delay samples,
// rounded to the nearest frame. Non-positive BPM or sample rate yields 0.
func DelaySamplesFor(bpm float64, sub Subdivision, sampleRate int) int {
	if !(bpm > 0) || sampleRate <= 0 || !(sub > 0) {
		return 0
	}
	secs := 60.0 / bpm * float64(sub)
	return int(math.Round(secs * float64(sampleRate)))
}

// DampingCoeff returns the 1-pole lowpass coefficient for cutoff fc Hz:
//
//	alpha = exp(-2*pi*fc/fs)
//
// The feedback tap filters as y = (1-alpha)*x + alpha*yPrev, so alpha
// near 1 darkens strongly and alpha near 0 stays bright. Non-positive
// cutoffs disable damping (alpha 0); cutoffs at or above Nyquist
// saturate near 1.
func DampingCoeff(fc float32, sampleRate int) float32 {
	if sampleRate <= 0 {
		return 0
	}
	if fc <= 0 {
		return 0
	}
	nyquist := float64(sampleRate) / 2
	f := float64(fc)
	if f >= nyquist {
		f = math.Nextafter(nyquist, 0)
	}
	return float32(math.Exp(-2 * math.Pi * f / float64(sampleRate)))
}

// DelayConfig describes one echo space. Subdivision with a positive BPM
// selects the delay time; DelaySeconds above zero overrides it directly.
type DelayConfig struct {
	Mode         DelayMode
	Subdivision  Subdivision // BPM-synced note value
	DelaySeconds float64     // manual override in seconds when > 0
	Feedback     float32     // 0.0 to 0.98, cross-fed in ping-pong mode
	DampingHz    float32     // 1-pole lowpass cutoff in the feedback path
	Wet          float32     // wet mix 0.0 to 1.0 for ProcessFrame
}

// StereoDelay is a studio-grade stereo echo: two power-of-two ring lines
// sharing one delay length, a 1-pole lowpass in the feedback path for
// analog tape warmth, and stereo or ping-pong cross-feedback. All buffers
// are pre-allocated; processing and configuration perform no allocations.
type StereoDelay struct {
	bufferL, bufferR []float32
	mask             int
	writeIdx         int
	lengthSamples    int

	feedback   float32
	dampCoeff  float32
	dampStateL float32
	dampStateR float32
	wet        float32
	mode       DelayMode

	sampleRate int
	maxSamples int
}

// NewStereoDelay pre-allocates ring buffers holding up to maxSeconds of
// echo at sampleRate Hz. Non-positive inputs fall back to 48 kHz and
// MaxDelaySeconds. Capacity rounds up to a power of two for fast masking.
func NewStereoDelay(sampleRate int, maxSeconds float64) *StereoDelay {
	if sampleRate <= 0 {
		sampleRate = 48000
	}
	if !(maxSeconds > 0) {
		maxSeconds = MaxDelaySeconds
	}
	n := int(float64(sampleRate)*maxSeconds) + 1
	if n < 2 {
		n = 2
	}
	size := 1
	for size < n {
		size <<= 1
	}
	return &StereoDelay{
		bufferL:    make([]float32, size),
		bufferR:    make([]float32, size),
		mask:       size - 1,
		sampleRate: sampleRate,
		maxSamples: size,
		wet:        0.4,
	}
}

// Configure retunes the echo in place without allocating. When cfg
// carries DelaySeconds above zero it wins; otherwise the subdivision is
// resolved against bpm. Feedback clamps to [0, 0.98], wet to [0, 1], and
// non-positive damping falls back to DefaultDelayDampingHz. A resolved
// length of zero bypasses the network while keeping state valid.
func (d *StereoDelay) Configure(cfg DelayConfig, bpm float64) {
	fb := ClampF32(cfg.Feedback, 0, 0.98)
	wet := ClampF32(cfg.Wet, 0, 1)
	dampHz := cfg.DampingHz
	if dampHz <= 0 {
		dampHz = DefaultDelayDampingHz
	}
	length := 0
	if cfg.DelaySeconds > 0 {
		length = int(math.Round(cfg.DelaySeconds * float64(d.sampleRate)))
	} else if cfg.Subdivision > 0 && bpm > 0 {
		length = DelaySamplesFor(bpm, cfg.Subdivision, d.sampleRate)
	}
	if length < 0 {
		length = 0
	}
	if length >= d.maxSamples {
		length = d.maxSamples - 1
	}
	d.lengthSamples = length
	d.feedback = fb
	d.wet = wet
	d.mode = cfg.Mode
	d.dampCoeff = DampingCoeff(dampHz, d.sampleRate)
}

// SetDelaySamples overrides the tap length directly in frames, clamped to
// the pre-allocated capacity. It never allocates.
func (d *StereoDelay) SetDelaySamples(n int) {
	if n < 0 {
		n = 0
	}
	if n >= d.maxSamples {
		n = d.maxSamples - 1
	}
	d.lengthSamples = n
}

// LengthSamples reports the active delay length in frames.
func (d *StereoDelay) LengthSamples() int { return d.lengthSamples }

// MemoryBytes reports the pre-allocated ring storage in bytes: two
// power-of-two float32 lines. At 48 kHz with a 4 s maximum this is about
// 2 MB per instance, allocated once at construction.
func (d *StereoDelay) MemoryBytes() int { return 2 * len(d.bufferL) * 4 }

// SampleRate reports the configured rate in Hz.
func (d *StereoDelay) SampleRate() int { return d.sampleRate }

// Mode reports the feedback topology.
func (d *StereoDelay) Mode() DelayMode { return d.mode }

// Feedback reports the loop gain.
func (d *StereoDelay) Feedback() float32 { return d.feedback }

// Wet reports the mix level used by ProcessFrame.
func (d *StereoDelay) Wet() float32 { return d.wet }

// DampingHz inverts the coefficient back to an approximate cutoff in Hz.
func (d *StereoDelay) DampingHz() float32 {
	if d.dampCoeff <= 0 {
		return 0
	}
	if d.dampCoeff >= 1 {
		return float32(d.sampleRate) / 2
	}
	return float32(-math.Log(float64(d.dampCoeff)) * float64(d.sampleRate) / (2 * math.Pi))
}

// Reset clears both lines and the damping state.
func (d *StereoDelay) Reset() {
	clear(d.bufferL)
	clear(d.bufferR)
	d.writeIdx = 0
	d.dampStateL = 0
	d.dampStateR = 0
}

// ProcessFrameWet runs one stereo frame through the echo and returns the
// wet signal only (send/return style, as used by the mixer). The tap is
// read, warmed by the 1-pole damper, crossed through the feedback matrix
// in ping-pong mode, and written back with fresh input:
//
//	stereo:    writeL = inL + dampL*fb, writeR = inR + dampR*fb
//	ping-pong: writeL = inL + dampR*fb, writeR = inR + dampL*fb
func (d *StereoDelay) ProcessFrameWet(inL, inR float32) (float32, float32) {
	if d.lengthSamples <= 0 {
		return 0, 0
	}
	readIdx := (d.writeIdx - d.lengthSamples) & d.mask
	rawL := d.bufferL[readIdx]
	rawR := d.bufferR[readIdx]

	// Analog warmth: 1-pole lowpass per tap.
	// y = (1-alpha)*x + alpha*yPrev.
	oneMinus := 1 - d.dampCoeff
	d.dampStateL = oneMinus*rawL + d.dampCoeff*d.dampStateL
	d.dampStateR = oneMinus*rawR + d.dampCoeff*d.dampStateR
	wetL, wetR := d.dampStateL, d.dampStateR

	if d.mode == DelayModePingPong {
		d.bufferL[d.writeIdx] = inL + wetR*d.feedback
		d.bufferR[d.writeIdx] = inR + wetL*d.feedback
	} else {
		d.bufferL[d.writeIdx] = inL + wetL*d.feedback
		d.bufferR[d.writeIdx] = inR + wetR*d.feedback
	}
	d.writeIdx = (d.writeIdx + 1) & d.mask
	return wetL, wetR
}

// ProcessFrame returns one frame of the wet/dry mix:
//
//	out = (1-wet)*dry + wet*delayOut
func (d *StereoDelay) ProcessFrame(inL, inR float32) (float32, float32) {
	wetL, wetR := d.ProcessFrameWet(inL, inR)
	return inL*(1-d.wet) + wetL*d.wet, inR*(1-d.wet) + wetR*d.wet
}

// ProcessStereo processes a planar stereo buffer in place with the
// wet/dry mix.
func (d *StereoDelay) ProcessStereo(buf *StereoBuffer) {
	if buf == nil {
		return
	}
	left, right := buf.Left, buf.Right
	n := len(left)
	if len(right) < n {
		n = len(right)
	}
	for i := 0; i < n; i++ {
		left[i], right[i] = d.ProcessFrame(left[i], right[i])
	}
}

// ProcessInterleaved processes interleaved stereo frames in place with
// the wet/dry mix, the layout used by the mixer buses.
func (d *StereoDelay) ProcessInterleaved(buf []float32) {
	for i := 0; i+1 < len(buf); i += 2 {
		buf[i], buf[i+1] = d.ProcessFrame(buf[i], buf[i+1])
	}
}
