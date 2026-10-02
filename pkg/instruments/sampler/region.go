// Package sampler implements a polyphonic SFZ sample player: a universal
// fault-tolerant SFZ parser, WAV sample regions, and a fixed-voice-count
// player with voice stealing.
package sampler

import "math"

// Loop modes recognized in Region.LoopMode. Only loop_continuous engages
// playback looping; any other stored value behaves as no loop.
const (
	LoopNoLoop     = "no_loop"
	LoopContinuous = "loop_continuous"
	LoopOneShot    = "one_shot" // plays to the sample end, ignoring note-off
)

// EGParams holds one SFZ envelope generator. Times are seconds, Sustain
// is a percentage (0-100), and Depth is target-specific (cents for
// pitch, dB for filter). A family keeps SFZ defaults until any of its
// opcodes appears; see Region.HasAmpEG and friends.
type EGParams struct {
	Delay, Start, Attack, Hold, Decay, Sustain, Release         float32
	Vel2Attack, Vel2Decay, Vel2Release, Key2Attack, Key2Release float32
	Depth, Vel2Depth                                            float32
}

// LFOParams holds one SFZ low-frequency oscillator.
type LFOParams struct {
	Delay, Fade, Freq                                   float32
	Volume, Amplitude, Pitch, Filter, Pan, VolumeSmooth float32
	Wave, FreqWave                                      int
}

// EQBand holds one SFZ equalizer band.
type EQBand struct {
	Freq, BW, Gain, Veltrack float32
}

// Region is one SFZ zone: a sample mapped onto a key and velocity range
// with playback modifiers.
//
// Playback applies: key/velocity mapping, the sample window (Offset/End),
// Transpose/Tune, looping, the amp envelope (when HasAmpEG), Volume/Pan,
// and OffBy voice cutoff. The remaining parsed parameters (filter, EQ,
// LFOs, pitch/filter envelopes, keyswitches, polyphony limits) are
// stored verbatim for inspection and future DSP wiring.
type Region struct {
	SamplePath     string
	LoKey          int
	HiKey          int
	PitchKeyCenter int
	LoVel          int
	HiVel          int
	Volume         float32 // dB, per SFZ convention
	Pan            float32 // normalized to [-1,1] from SFZ percent (-100..100)
	Sample         *SampleData

	// Sample window and looping. LoopStart/LoopEnd are -1 when no opcode
	// set them; the resolved loop points used at playback live in
	// SampleData. End is the exclusive end frame; 0 means whole sample.
	Offset        int
	End           int
	Count         int // -1 unset, 0 = no loop, N>0 = N loop passes
	LoopMode      string
	LoopStart     int
	LoopEnd       int
	LoopType      int
	LoopCount     int
	LoopTune      int // cents
	LoopCrossfade int // ms
	Reverse       int

	// Velocity and key crossfade zones (xfin/xfout). A zone is active
	// when its bounds differ; equal bounds mean no fade on that side.
	// Equal-power cosine (out) / sine (in) gains blend overlapping
	// layers so their energies sum to unity.
	XfinLoKey  int
	XfinHiKey  int
	XfinLoVel  int
	XfinHiVel  int
	XfoutLoKey int
	XfoutHiKey int
	XfoutLoVel int
	XfoutHiVel int

	// Pitch.
	PitchKeytrack int // percent, default 100
	Transpose     int // semitones
	Tune          int // cents
	PitchRandom   int // cents
	PitchVeltrack int // cents
	BendUp        int // cents
	BendDown      int // cents
	BendStep      int // cents

	// Amplitude behavior.
	AmpRandom    float32
	AmpVeltrack  float32
	AmpKeytrack  float32
	AmpKeycenter int
	RtDecay      float32

	// Envelopes, LFOs, filter, and EQ.
	AmpEG, PitchEG, FilEG          EGParams
	HasAmpEG, HasPitchEG, HasFilEG bool
	LFO                            [2]LFOParams
	FilType                        string
	Cutoff                         float32 // Hz; -1 unset
	Resonance                      float32
	FilKeytrack                    float32
	FilKeycenter                   int
	FilVeltrack                    float32
	FilRandom                      float32
	EQ                             [3]EQBand

	// Voice behavior.
	OffBy         int
	OffMode       string
	Polyphony     int
	NotePolyphony int
	Trigger       string
	GroupLabel    string

	// Round-robin sequence cycling. SeqLength 1 (the default) disables
	// cycling; SeqPosition is the 1-based step this region occupies.
	SeqLength   int
	SeqPosition int

	// Probabilistic selection window. A region plays only when the
	// per-note-on roll in [0,1) falls inside [LoRand, HiRand]. The
	// default 0.0-1.0 window keeps regions without these opcodes always
	// eligible.
	LoRand float32
	HiRand float32

	// Keyswitching (stored; articulation switching is not applied).
	SwLoKey, SwHiKey, SwDefault int // -1 unset
	SwLast, SwDown, SwUp        int // -1 unset
}

// SampleData is a decoded sample. Samples holds mono float32 frames:
// multi-channel sources are averaged at load and Channels records the
// source layout. Loop points are frame indices (LoopEnd exclusive) taken
// from the WAV smpl chunk or SFZ opcodes; 0 means no loop.
type SampleData struct {
	Samples    []float32
	SampleRate int
	Channels   int
	LoopStart  int
	LoopEnd    int
}

// Frames returns the frame count of the sample.
func (sd *SampleData) Frames() int { return len(sd.Samples) }

// newRegion returns a region pre-filled with the SFZ default opcodes.
func newRegion() Region {
	return Region{
		LoKey: 0, HiKey: 127, PitchKeyCenter: 60, PitchKeytrack: 100,
		LoVel: 0, HiVel: 127,
		Volume: 0, Pan: 0,
		Count:      -1,
		LoopMode:   LoopNoLoop,
		LoopStart:  -1,
		LoopEnd:    -1,
		XfoutLoKey: 127, XfoutHiKey: 127,
		XfoutLoVel: 127, XfoutHiVel: 127,
		Cutoff:       -1,
		FilKeycenter: 60,
		AmpKeycenter: 60,
		OffMode:      "fast",
		SeqLength:    1,
		SeqPosition:  1,
		LoRand:       0,
		HiRand:       1,
		SwLoKey:      -1,
		SwHiKey:      -1,
		SwDefault:    -1,
		SwLast:       -1,
		SwDown:       -1,
		SwUp:         -1,
	}
}

// ensureAmpEG initializes the amp envelope to SFZ defaults the first
// time any ampeg opcode appears.
func (r *Region) ensureAmpEG() {
	if !r.HasAmpEG {
		r.AmpEG = EGParams{Sustain: 100}
		r.HasAmpEG = true
	}
}

// ensurePitchEG initializes the pitch envelope to SFZ defaults.
func (r *Region) ensurePitchEG() {
	if !r.HasPitchEG {
		r.PitchEG = EGParams{}
		r.HasPitchEG = true
	}
}

// ensureFilEG initializes the filter envelope to SFZ defaults.
func (r *Region) ensureFilEG() {
	if !r.HasFilEG {
		r.FilEG = EGParams{}
		r.HasFilEG = true
	}
}

// Matches reports whether the region plays for the given MIDI key and
// velocity (both 0-127).
func (r *Region) Matches(pitch, velocity int) bool {
	return r.Sample != nil &&
		pitch >= r.LoKey && pitch <= r.HiKey &&
		velocity >= r.LoVel && velocity <= r.HiVel
}

// LinearGain converts the region volume from dB to linear gain.
func (r *Region) LinearGain() float32 {
	return float32(math.Pow(10, float64(r.Volume)/20))
}

// PlaybackRateFor returns the source-frame advance per output frame for
// the given pitch rendered at dstRate, including the region's transpose
// (semitones) and tune (cents):
//
//	rate = 2^((pitch - pitch_keycenter + transpose + tune/100)/12)
//	       · sampleRate/dstRate
func (r *Region) PlaybackRateFor(pitch int, dstRate float64) float64 {
	if r.Sample == nil || dstRate <= 0 {
		return 0
	}
	semitones := float64(pitch-r.PitchKeyCenter+r.Transpose) + float64(r.Tune)/100
	return math.Pow(2, semitones/12) * float64(r.Sample.SampleRate) / dstRate
}
