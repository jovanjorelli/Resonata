package dsp

import "math"

// Mastering limiter defaults.
const (
	// DefaultLookahead is the detector-to-output delay: 5 ms, i.e. 240
	// frames at 48 kHz (ring capacity padded to 256 for mask wrapping).
	DefaultLookahead = 0.005

	DefaultLimiterThresholdDB = -6.0
	DefaultLimiterKneeDB      = 6.0

	// DefaultCeiling is -0.1 dBFS (≈0.9886) rounded down so output
	// strictly never exceeds 0.988.
	DefaultCeiling = 0.988

	// DefaultTargetLUFS is the streaming loudness target for auto-gain.
	DefaultTargetLUFS = -14.0

	detectorAttackSeconds  = 0.0002 // envelope follower attack
	detectorReleaseSeconds = 0.06   // envelope follower release
	gainReleaseSeconds     = 0.1    // limiter gain recovery
	loudnessWindowSeconds  = 0.4    // loudness mean-square smoothing
	autoGainTimeSeconds    = 0.5    // auto-gain adaptation
	loudnessGateLUFS       = -50.0  // absolute gate for loudness updates
	loudnessCalibration    = 0.691  // K-weight offset at 1 kHz, approximate
	defaultMaxBoost        = 8.0    // auto-gain cap: +18 dB
	envFloor               = 1e-9   // divide-by-zero guard
)

// LookaheadBuffer is a power-of-two ring delay line holding stereo
// audio. Write stores the current frame and returns the frame from
// delay frames ago, so gain decisions computed from the undelayed signal
// can be applied before the peak ever reaches the output.
type LookaheadBuffer struct {
	bufL, bufR []float32 // Pre-allocated, capacity next power of two > delay
	writeIdx   int
	mask       int // For fast modulo if size is power of 2 (e.g., 256)
	delay      int // delay in frames (e.g. 240 at 48 kHz / 5 ms)
}

// NewLookaheadBuffer allocates a ring whose capacity is the next power
// of two above delay, guaranteeing reads never wrap into unwritten data.
func NewLookaheadBuffer(delay int) *LookaheadBuffer {
	if delay < 1 {
		delay = 1
	}
	capacity := 1
	for capacity <= delay {
		capacity <<= 1
	}
	return &LookaheadBuffer{
		bufL:  make([]float32, capacity),
		bufR:  make([]float32, capacity),
		mask:  capacity - 1,
		delay: delay,
	}
}

// Write stores one frame and returns the frame from delay frames ago.
func (d *LookaheadBuffer) Write(l, r float32) (float32, float32) {
	readIdx := (d.writeIdx - d.delay) & d.mask
	outL, outR := d.bufL[readIdx], d.bufR[readIdx]
	d.bufL[d.writeIdx] = l
	d.bufR[d.writeIdx] = r
	d.writeIdx = (d.writeIdx + 1) & d.mask
	return outL, outR
}

// Reset clears the delay line.
func (d *LookaheadBuffer) Reset() {
	clear(d.bufL)
	clear(d.bufR)
	d.writeIdx = 0
}

// Delay reports the delay length in frames.
func (d *LookaheadBuffer) Delay() int { return d.delay }

// Cap reports the ring capacity in frames.
func (d *LookaheadBuffer) Cap() int { return d.mask + 1 }

// Limiter is a transparent lookahead peak limiter for the master bus:
// a stereo-linked envelope follower on the undelayed signal, a soft-knee
// gain computer with a hard ceiling guarantee, and optional auto-gain
// driving an approximate LUFS loudness target. Orchestral material with
// ppp-to-fff dynamics reaches commercial loudness without crushed
// transients or digital clipping. All state is pre-allocated; the
// processing loop performs no allocations.
type Limiter struct {
	sampleRate float64

	// 5 ms lookahead: audio ring plus a mirrored ring carrying the
	// guaranteed envelope (>= instantaneous peak) of each frame. At exit
	// time the safety gain divides by the envelope captured when the
	// exiting sample entered, so out <= ceiling holds strictly.
	delay   LookaheadBuffer
	envRing []float32

	// Envelope follower state (spec one-pole peak detector).
	env      float32
	alphaAtt float32
	alphaRel float32

	// Gain computer.
	thresholdDB  float32
	kneeDB       float32
	kneeStartDB  float32
	kneeStart    float32 // linear knee start
	ceiling      float32
	gain         float32
	alphaGainRel float32

	// Auto-gain and loudness estimate.
	autoEnabled bool
	targetLUFS  float32
	targetMS    float32
	loudMS      float32
	loudGateMS  float32
	alphaLoud   float32
	alphaAuto   float32
	autoGain    float32
	maxBoost    float32
	loudHP      *Biquad
}

// NewLimiter returns a limiter for sampleRate Hz with 5 ms lookahead,
// -6 dBFS threshold, 6 dB soft knee, the 0.988 ceiling, and auto-gain
// toward -14 LUFS enabled.
func NewLimiter(sampleRate float64) *Limiter {
	if sampleRate <= 0 {
		sampleRate = 48000
	}
	delayFrames := int(math.Round(DefaultLookahead * sampleRate))
	delay := NewLookaheadBuffer(delayFrames)
	l := &Limiter{
		sampleRate:   sampleRate,
		delay:        *delay,
		envRing:      make([]float32, delay.Cap()),
		alphaAtt:     float32(math.Exp(-1.0 / (sampleRate * detectorAttackSeconds))),
		alphaRel:     float32(math.Exp(-1.0 / (sampleRate * detectorReleaseSeconds))),
		thresholdDB:  DefaultLimiterThresholdDB,
		kneeDB:       DefaultLimiterKneeDB,
		ceiling:      DefaultCeiling,
		gain:         1,
		alphaGainRel: float32(math.Exp(-1.0 / (sampleRate * gainReleaseSeconds))),
		autoEnabled:  true,
		targetLUFS:   DefaultTargetLUFS,
		loudGateMS:   float32(math.Pow(10, (loudnessGateLUFS-loudnessCalibration)/10)),
		alphaLoud:    float32(1 - math.Exp(-1.0/(sampleRate*loudnessWindowSeconds))),
		alphaAuto:    float32(1 - math.Exp(-1.0/(sampleRate*autoGainTimeSeconds))),
		autoGain:     1,
		maxBoost:     defaultMaxBoost,
		loudHP:       NewBiquad(Highpass, 60, defaultQ, sampleRate),
	}
	l.SetTargetLUFS(DefaultTargetLUFS)
	l.SetThresholdDB(DefaultLimiterThresholdDB)
	return l
}

// SetThresholdDB retunes the knee threshold in dBFS.
func (l *Limiter) SetThresholdDB(db float32) {
	l.thresholdDB = ClampF32(db, -60, 0)
	l.kneeStartDB = l.thresholdDB - l.kneeDB/2
	l.kneeStart = float32(math.Pow(10, float64(l.kneeStartDB)/20))
}

// ThresholdDB reports the knee threshold in dBFS.
func (l *Limiter) ThresholdDB() float32 { return l.thresholdDB }

// SetKneeDB sets the soft-knee width in dB.
func (l *Limiter) SetKneeDB(db float32) {
	l.kneeDB = ClampF32(db, 0, 24)
	l.SetThresholdDB(l.thresholdDB) // recompute the knee start
}

// SetCeiling sets the linear output ceiling, clamped to (0, 1].
func (l *Limiter) SetCeiling(c float32) {
	l.ceiling = ClampF32(c, 0.01, 1)
}

// Ceiling reports the linear output ceiling.
func (l *Limiter) Ceiling() float32 { return l.ceiling }

// SetAutoGain enables or disables the loudness auto-gain stage;
// disabling resets the pre-gain to unity.
func (l *Limiter) SetAutoGain(enabled bool) {
	l.autoEnabled = enabled
	if !enabled {
		l.autoGain = 1
		l.loudMS = 0
	}
}

// AutoGainEnabled reports whether auto-gain is active.
func (l *Limiter) AutoGainEnabled() bool { return l.autoEnabled }

// SetTargetLUFS sets the approximate loudness target for auto-gain.
func (l *Limiter) SetTargetLUFS(lufs float32) {
	l.targetLUFS = ClampF32(lufs, -40, 0)
	l.targetMS = float32(math.Pow(10, float64(l.targetLUFS-loudnessCalibration)/10))
}

// TargetLUFS reports the loudness target.
func (l *Limiter) TargetLUFS() float32 { return l.targetLUFS }

// Gain reports the current limiter gain (linear, <= 1).
func (l *Limiter) Gain() float32 { return l.gain }

// AutoGain reports the current auto-gain pre-amplifier (linear).
func (l *Limiter) AutoGain() float32 { return l.autoGain }

// GainReductionDB reports the current limiter gain reduction in dB
// (negative or zero), floored at -120 dB.
func (l *Limiter) GainReductionDB() float32 {
	if l.gain <= 1e-6 {
		return -120
	}
	return float32(20 * math.Log10(float64(l.gain)))
}

// LoudnessLUFS reports the gated loudness estimate of the source
// program (before auto-gain), in approximate LUFS (floor -70).
func (l *Limiter) LoudnessLUFS() float32 {
	if l.loudMS <= 1e-7 {
		return -70
	}
	v := float32(10*math.Log10(float64(l.loudMS)) + loudnessCalibration)
	if v < -70 {
		return -70
	}
	return v
}

// DelayFrames reports the lookahead delay in frames.
func (l *Limiter) DelayFrames() int { return l.delay.Delay() }

// Reset clears all delay lines, detectors, and gains.
func (l *Limiter) Reset() {
	l.delay.Reset()
	clear(l.envRing)
	l.env = 0
	l.gain = 1
	l.autoGain = 1
	l.loudMS = 0
	l.loudHP.Reset()
}

// ProcessStereo limits a planar stereo buffer in place.
func (l *Limiter) ProcessStereo(buf *StereoBuffer) {
	if buf == nil {
		return
	}
	left, right := buf.Left, buf.Right
	n := len(left)
	if len(right) < n {
		n = len(right)
	}
	for i := 0; i < n; i++ {
		left[i], right[i] = l.processFrame(left[i], right[i])
	}
}

// ProcessInterleaved limits interleaved stereo frames in place, the
// layout used by the mixer master bus.
func (l *Limiter) ProcessInterleaved(b []float32) {
	for i := 0; i+1 < len(b); i += 2 {
		b[i], b[i+1] = l.processFrame(b[i], b[i+1])
	}
}

// processFrame limits one stereo frame:
//
//	x      = in · autoGain                       (pre-gain)
//	env    = one-pole peak follower on |x|       (undelayed, spec formula)
//	target = min(softKnee(env), ceiling/envDelayed)
//	gain   -> target instantly down, smoothed up
//	out    = x delayed 5 ms · gain, clamped to ±ceiling
func (l *Limiter) processFrame(inL, inR float32) (float32, float32) {
	// Loudness estimate of the SOURCE program (before pre-gain) drives
	// auto-gain; measuring post-gain would make the loop self-canceling.
	l.updateLoudness((inL + inR) * 0.5)

	g := l.autoGain
	xL, xR := inL*g, inR*g

	// Stereo-linked peak detector (one-pole follower, spec form):
	//   env = input + alpha·(env - input), alpha_att when rising.
	peak := absF32(xL)
	if a := absF32(xR); a > peak {
		peak = a
	}
	if peak > l.env {
		l.env = peak + l.alphaAtt*(l.env-peak)
	} else {
		l.env = peak + l.alphaRel*(l.env-peak)
	}

	// Guaranteed envelope: never below the instantaneous peak, captured
	// into the ring mirroring the audio delay.
	guaranteed := l.env
	if peak > guaranteed {
		guaranteed = peak
	}
	readIdx := (l.delay.writeIdx - l.delay.delay) & l.delay.mask
	envDelayed := l.envRing[readIdx]
	l.envRing[l.delay.writeIdx] = guaranteed

	// Gain computer: soft knee for musical compression, hard safety cap
	// from the delayed envelope, instant reduction, smooth recovery.
	target := l.kneeGain(l.env)
	if envDelayed > envFloor {
		if safe := l.ceiling / envDelayed; safe < target {
			target = safe
		}
	}
	if target > 1 {
		target = 1
	}
	if target < l.gain {
		l.gain = target
	} else {
		l.gain += l.alphaGainRel * (target - l.gain)
	}

	// Apply the gain to the audio exiting the lookahead delay.
	outL, outR := l.delay.Write(xL, xR)
	outL = clampCeiling(outL*l.gain, l.ceiling)
	outR = clampCeiling(outR*l.gain, l.ceiling)
	return outL, outR
}

// kneeGain is the static soft-knee characteristic: unity below the knee,
// a quadratic blend inside it, and slope -1 (peak limiting) above the
// threshold.
func (l *Limiter) kneeGain(env float32) float32 {
	if env <= l.kneeStart {
		return 1
	}
	envDB := float32(20 * math.Log10(float64(env)))
	var gainDB float32
	if envDB <= l.thresholdDB+l.kneeDB/2 {
		x := envDB - l.kneeStartDB
		gainDB = -x * x / (2 * l.kneeDB)
	} else {
		gainDB = -(envDB - l.thresholdDB)
	}
	return float32(math.Pow(10, float64(gainDB)/20))
}

// updateLoudness folds one mono sample into the gated loudness estimate
// and adapts the auto-gain pre-amplifier toward the LUFS target.
func (l *Limiter) updateLoudness(mono float32) {
	if !l.autoEnabled {
		return
	}
	hp := l.loudHP.ProcessSample(mono)
	inst := hp * hp
	if inst <= l.loudGateMS && l.loudMS <= l.loudGateMS {
		return // gated: silence does not drag the estimate down
	}
	l.loudMS += l.alphaLoud * (inst - l.loudMS)
	if l.loudMS <= l.loudGateMS {
		return
	}
	desired := float32(math.Sqrt(float64(l.targetMS / l.loudMS)))
	desired = ClampF32(desired, 1/l.maxBoost, l.maxBoost)
	l.autoGain += l.alphaAuto * (desired - l.autoGain)
}

// absF32 returns the magnitude of v.
func absF32(v float32) float32 {
	if v < 0 {
		return -v
	}
	return v
}

// clampCeiling bounds v to [-c, c]; a float-rounding guard so the output
// strictly never exceeds the ceiling.
func clampCeiling(v, c float32) float32 {
	if v > c {
		return c
	}
	if v < -c {
		return -c
	}
	return v
}
