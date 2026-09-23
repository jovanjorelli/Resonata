package sampler

import (
	"math"

	"resonata/pkg/dsp"
	"resonata/pkg/instruments"
)

// Sampler satisfies the instruments.Instrument contract.
var _ instruments.Instrument = (*Sampler)(nil)

// Sampler voices the full per-note payload: envelope overrides,
// vibrato, and expression.
var _ instruments.ParamsVoicer = (*Sampler)(nil)

// MaxVoices is the polyphony limit per sampler instance.
const MaxVoices = 16

// defaultRandSeed seeds the note-on LCG. It is fixed (never the wall
// clock) so offline renders are deterministic: the same score always
// produces the same audio. Reseed with SetRandSeed for take variation.
const defaultRandSeed uint64 = 0x243F6A8885A308D3

// Default voice envelope (seconds / level). Sampled instruments rely on
// the recording for the body of the note, so sustain is flat and the
// release is a short anti-click fade.
const (
	defaultAttack  = 0.005
	defaultDecay   = 1e-6
	defaultSustain = 1.0
	defaultRelease = 0.20

	mixScratchFrames = 2048
)

// loopCrossfadeSamples is the equal-power loop crossfade length in
// source frames, about 8 ms at 48 kHz. Loops shorter than this fall
// back to a hard wrap.
const loopCrossfadeSamples = 384

// stealFadeSamples is the voice-steal fade-out length in output frames,
// about 5 ms at 48 kHz. stealFadeStep advances the 0-to-1 fade progress
// once per rendered frame.
const stealFadeSamples = 240
const stealFadeStep = float32(1.0 / stealFadeSamples)

// autoFadeSamples is the same-pitch auto-fade length in output frames,
// about 10 ms at 48 kHz: long enough to hide the seam, short enough to
// keep rhythmic precision. autoFadeStep advances that fade per frame.
const autoFadeSamples = 480
const autoFadeStep = float32(1.0 / autoFadeSamples)

// maxPitchVoices caps the tracked voices per MIDI pitch. Past the cap,
// note-on falls back to a full voice scan, so tracking can never hide
// a sounding voice.
const maxPitchVoices = 8

// Vibrato engagement: the LFO stays silent for vibDelay seconds after
// note-on (and during the attack), then ramps to full depth over
// vibRamp seconds so the onset never clicks in.
const (
	vibDelay = 0.15
	vibRamp  = 0.10
	twoPi    = 2 * math.Pi
)

// Voice is one polyphonic playback stream over a region's sample. Phase
// is the read position in source frames; PlaybackRate is the source-frame
// advance per output frame (see Region.PlaybackRateFor). ReleaseTime is
// the sampler-clock timestamp at which the voice entered release, used to
// pick the oldest releasing voice when stealing.
type Voice struct {
	Region       *Region
	Pitch        int
	Velocity     float32
	Phase        float64
	PlaybackRate float64
	Envelope     *dsp.ADSR
	Active       bool
	ReleaseTime  float64

	// Render state derived at note-on.
	releasing  bool
	startTime  float64
	gain       float32 // region volume, linear
	panL, panR float32
	looping    bool
	loopStart  float64
	loopEnd    float64
	end        float64 // exclusive end frame; 0 = whole sample

	// Loop crossfade state. When a looping voice plays the last
	// loopCrossfadeSamples source frames of its loop, it mixes the
	// outgoing tail with an incoming stream from the loop start using
	// equal-power cosine/sine gains, hiding the loop seam.
	inCrossfade      bool
	crossfadePhase   float64 // 0.0 at crossfade entry, 1.0 at the boundary
	crossfadeReadPos float64 // incoming-stream read position from loop start

	// Voice-steal fade state. When the allocator reuses a sounding
	// voice, the old note fades to zero over stealFadeSamples frames
	// before the pending note starts, avoiding a hard cut. Same-pitch
	// auto-fades instead free the voice after autoFadeSamples frames.
	isStealing        bool
	stealFadeProgress float32 // 0.0 to 1.0 fade-out progress
	fadeStep          float32 // per-frame progress: stealFadeStep or autoFadeStep
	stealFree         bool    // fade to silence and free instead of repurposing
	pendingPitch      int     // MIDI note number starting after the fade
	pendingVelocity   float32 // velocity starting after the fade

	// Pending steal parameters captured at steal time so the voice can
	// reinitialize itself without heap allocation or sampler access.
	pendingRegion     *Region
	pendingRelease    bool // note-off for the pending pitch arrived mid-fade
	pendingParams     instruments.NoteParams
	stealSampleRate   float64
	pendingAttack     float64
	pendingDecay      float64
	pendingSustain    float64
	pendingReleaseEnv float64
	pendingStartTime  float64

	// Crossfade gain from the region's xfin/xfout zones at trigger
	// velocity and pitch; 1.0 plays full.
	crossfadeGain float32

	// Vibrato and expression state set at note-on. The LFO modulates
	// the playback rate around the base pitch; expression scales the
	// output after the envelope, independently of attack velocity.
	hasVibrato bool
	vibRate    float64 // LFO frequency in Hz
	vibDepthM  float64 // 2^(depth/12)-1: fractional rate swing at full LFO
	vibPhase   float64 // LFO angle in radians, starts at zero per note
	vibTimer   float64 // seconds since note-on, gates the 150 ms delay
	exprGain   float32 // sustained-dynamic multiplier, 1.0 is full volume

	// Per-pitch tracking. ptrack points at the sampler's fixed 128-entry
	// table; trackedPitch is the pitch this voice is registered under,
	// or -1 when the voice holds no slot.
	ptrack       *[128]pitchTracker
	trackedPitch int
}

// pitchTracker is one MIDI pitch's fixed voice roster: up to
// maxPitchVoices sounding voice pointers plus the live count. The
// table is allocated once with the sampler; add/remove swap within the
// fixed array and never allocate.
type pitchTracker struct {
	voices [maxPitchVoices]*Voice
	n      int
}

// Process renders this voice into the mono buffer following the sample
// playback algorithm: 4-point cubic Hermite (Catmull-Rom) interpolation
// between source frames, an equal-power loop crossfade at the loop seam,
// a vibrato LFO on the playback rate past the engagement delay,
// ADSR envelope, velocity and expression scaling, and the voice-steal
// fade-out. When a
// looping voice enters the last loopCrossfadeSamples source frames of
// its loop (loops shorter than that use a hard wrap), the output mixes
// the outgoing tail with an incoming stream from the loop start with
// cosine/sine gains so the seam stays continuous. When the voice was
// stolen, the old note scales by (1 - stealFadeProgress) over
// stealFadeSamples frames; at progress 1.0 the pending note starts from
// its attack, so the handoff is seamless. deltaTime is the block
// duration in seconds; the envelope advances per frame. Frames past the
// end of a non-looping sample, or after a completed release, deactivate
// the voice and are zero-filled. The loop allocates nothing.
func (v *Voice) Process(buffer []float32, deltaTime float64) {
	if !v.Active || v.Region == nil || v.Region.Sample == nil || len(buffer) == 0 || deltaTime <= 0 {
		return
	}
	samples := v.Region.Sample.Samples
	end := v.end
	if end <= 0 || end > float64(len(samples)) {
		end = float64(len(samples))
	}
	dt := deltaTime / float64(len(buffer))
	const xfLen = float64(loopCrossfadeSamples)
	i := 0
	for ; i < len(buffer); i++ {
		// A fade that completed on the previous frame hands off to the
		// pending note before rendering this frame.
		if v.isStealing && v.stealFadeProgress >= 1 {
			v.finishSteal()
			if !v.Active || v.Region == nil || v.Region.Sample == nil {
				break
			}
			samples = v.Region.Sample.Samples
			end = v.end
			if end <= 0 || end > float64(len(samples)) {
				end = float64(len(samples))
			}
		}
		index := int(v.Phase)
		if index >= len(samples) { // defensive: window or rate overshot
			if v.isStealing && v.pendingRegion != nil {
				// The old sample ran out mid-fade: the old note is
				// already silent, so start the pending note at once.
				v.stealFadeProgress = 1
				continue
			}
			v.Active = false
			v.trackUnregister()
			break
		}
		// Loop-crossfade sample read with loop-clamped spline taps.
		var sample float64
		loopLen := v.loopEnd - v.loopStart
		useXF := v.looping && loopLen >= xfLen && v.loopEnd <= float64(len(samples)) && v.loopStart >= 0
		if useXF {
			xfStart := v.loopEnd - xfLen
			if !v.inCrossfade && v.Phase >= xfStart && v.Phase < v.loopEnd {
				v.inCrossfade = true
				v.crossfadeReadPos = v.loopStart + (v.Phase - xfStart)
			}
			if v.inCrossfade {
				t := (v.Phase - xfStart) / xfLen
				if t < 0 {
					t = 0
				} else if t > 1 {
					t = 1
				}
				v.crossfadePhase = t
				lo := int(v.loopStart)
				hi := int(v.loopEnd)
				out := interpSampleBounded(samples, v.Phase, lo, hi)
				in := interpSampleBounded(samples, v.crossfadeReadPos, lo, hi)
				outGain := math.Cos(t * math.Pi / 2)
				inGain := math.Sin(t * math.Pi / 2)
				sample = out*outGain + in*inGain
			} else {
				sample = interpSample(samples, v.Phase)
			}
		} else {
			v.inCrossfade = false
			sample = interpSample(samples, v.Phase)
		}

		env := v.Envelope.Next(dt)
		out := sample * env * float64(v.Velocity) * float64(v.exprGain) * float64(v.crossfadeGain)

		// Steal fade-out multiplies the envelope gain and always wins:
		// it completes on its own schedule (general steal or the longer
		// auto-fade) whatever the release. The loop crossfade underneath
		// keeps running, scaled by the same gain, so the two mechanisms
		// never conflict.
		if v.isStealing {
			fadeGain := 1 - float64(v.stealFadeProgress)
			if fadeGain < 0 {
				fadeGain = 0
			}
			out *= fadeGain
			step := v.fadeStep
			if step <= 0 {
				step = stealFadeStep
			}
			v.stealFadeProgress += step
			if v.stealFadeProgress >= 1 {
				v.stealFadeProgress = 1
			}
		}
		buffer[i] = float32(out)

		if v.Envelope.Stage() == dsp.StageIdle {
			if v.isStealing && v.pendingRegion != nil {
				// The old envelope died mid-fade: hand off now.
				v.stealFadeProgress = 1
				continue
			}
			v.Active = false
			v.trackUnregister()
			i++
			break
		}

		// Vibrato LFO: silent during the attack and for 150 ms after
		// note-on, then ramping to full depth over 100 ms. The phase
		// advances every frame so the onset is continuous.
		rateEff := v.PlaybackRate
		if v.hasVibrato {
			v.vibPhase += twoPi * v.vibRate * dt
			v.vibTimer += dt
			if v.Envelope.Stage() != dsp.StageAttack && v.vibTimer >= vibDelay {
				ramp := (v.vibTimer - vibDelay) / vibRamp
				if ramp > 1 {
					ramp = 1
				}
				rateEff *= 1 + v.vibDepthM*math.Sin(v.vibPhase)*ramp
			}
		}

		if v.inCrossfade {
			v.Phase += rateEff
			v.crossfadeReadPos += rateEff
			if v.Phase >= v.loopEnd {
				// The boundary is crossed: continue from the incoming
				// stream, already past the loop start.
				v.Phase = v.crossfadeReadPos
				v.inCrossfade = false
			}
		} else {
			v.Phase += rateEff
			switch {
			case v.looping && v.Phase >= v.loopEnd:
				v.Phase -= loopLen
			case v.Phase >= end:
				if v.isStealing && v.pendingRegion != nil {
					v.stealFadeProgress = 1
					continue
				}
				v.Active = false
				v.trackUnregister()
				i++
			}
		}
		if !v.Active {
			break
		}
	}
	clear(buffer[i:])
}

// trackRegister moves the voice onto pitch's roster, leaving any old
// registration behind. Re-registering the tracked pitch only fills a
// missing slot, so the call is idempotent. Past the slot cap the voice
// plays untracked; note-on covers it with a full scan.
func (v *Voice) trackRegister(pitch int) {
	if v.trackedPitch == pitch {
		if v.ptrack == nil || pitch < 0 || pitch > 127 {
			return
		}
		t := &v.ptrack[pitch]
		for k := 0; k < t.n; k++ {
			if t.voices[k] == v {
				return
			}
		}
		if t.n < maxPitchVoices {
			t.voices[t.n] = v
			t.n++
		}
		return
	}
	v.trackUnregister()
	if v.ptrack == nil || pitch < 0 || pitch > 127 {
		return
	}
	t := &v.ptrack[pitch]
	if t.n < maxPitchVoices {
		t.voices[t.n] = v
		t.n++
	}
	v.trackedPitch = pitch
}

// trackUnregister drops the voice from its roster slot, if any.
func (v *Voice) trackUnregister() {
	if v.ptrack == nil || v.trackedPitch < 0 || v.trackedPitch > 127 {
		v.trackedPitch = -1
		return
	}
	t := &v.ptrack[v.trackedPitch]
	for k := 0; k < t.n; k++ {
		if t.voices[k] == v {
			t.n--
			t.voices[k] = t.voices[t.n]
			t.voices[t.n] = nil
			break
		}
	}
	v.trackedPitch = -1
}

// finishSteal reinitializes the voice for its pending note after the
// fade-out completes, or frees a fade-to-free voice. It runs inside the
// render loop, uses only steal-time captures, and allocates nothing. A
// pending note-off that arrived mid-fade releases the fresh note at once.
func (v *Voice) finishSteal() {
	if v.stealFree {
		v.Active = false
		v.isStealing = false
		v.stealFree = false
		v.trackUnregister()
		return
	}
	rg := v.pendingRegion
	pitch := v.pendingPitch
	vel := v.pendingVelocity
	sr := v.stealSampleRate
	if sr <= 0 {
		sr = 48000
	}
	wantRelease := v.pendingRelease
	pendingParams := v.pendingParams
	initVoice(v, rg, pitch, vel, sr,
		v.pendingAttack, v.pendingDecay, v.pendingSustain, v.pendingReleaseEnv,
		v.pendingStartTime, pendingParams)
	if wantRelease && rg != nil && rg.LoopMode != LoopOneShot {
		v.Envelope.Release()
		v.releasing = true
	}
}

// Sampler is a polyphonic SFZ sample player. Voices, envelopes, the
// mono mix scratch, and the per-key round-robin counters are
// pre-allocated; the playback path performs no allocations once the
// scratch has grown to the largest block seen.
type Sampler struct {
	sampleRate float64
	sfz        *SFZFile
	voices     [MaxVoices]Voice
	envs       [MaxVoices]dsp.ADSR
	mix        []float32
	clock      float64           // seconds since creation, orders voice stealing
	seq        [128]int          // per-key round-robin steps, one counter per MIDI note
	pitchTrack [128]pitchTracker // fixed per-pitch voice rosters, zero-alloc
	trig       []xfTrigger       // collected voice requests per note-on, reused
	rand       uint64            // note-on LCG state for lorand/hirand rolls

	// Envelope defaults applied when a voice triggers.
	attack  float64
	decay   float64
	sustain float64
	release float64

	masterGain float32
}

// New returns an empty sampler rendering at sampleRate Hz. Load a parsed
// SFZ definition (or use LoadFile) before playing notes.
func New(sampleRate float64) *Sampler {
	if sampleRate <= 0 {
		sampleRate = 48000
	}
	s := &Sampler{
		sampleRate: sampleRate,
		attack:     defaultAttack,
		decay:      defaultDecay,
		sustain:    defaultSustain,
		release:    defaultRelease,
		masterGain: 1,
		rand:       defaultRandSeed,
		mix:        make([]float32, mixScratchFrames),
	}
	for i := range s.voices {
		s.voices[i].Envelope = &s.envs[i]
		s.voices[i].Envelope.SetParameters(s.attack, s.decay, s.sustain, s.release)
		s.voices[i].ptrack = &s.pitchTrack
		s.voices[i].trackedPitch = -1
		s.voices[i].fadeStep = stealFadeStep
	}
	return s
}

// LoadFile parses the SFZ file at path and returns a ready sampler.
func LoadFile(path string, sampleRate float64) (*Sampler, error) {
	f, err := LoadSFZ(path)
	if err != nil {
		return nil, err
	}
	s := New(sampleRate)
	s.Load(f)
	return s, nil
}

// Load binds an SFZ definition, silencing all voices and restarting
// every round-robin sequence at position 1.
func (s *Sampler) Load(f *SFZFile) {
	s.sfz = f
	for i := range s.voices {
		v := &s.voices[i]
		v.Active = false
		v.releasing = false
		v.inCrossfade = false
		v.crossfadePhase = 0
		v.crossfadeReadPos = 0
		v.isStealing = false
		v.stealFadeProgress = 0
		v.fadeStep = stealFadeStep
		v.stealFree = false
		v.pendingRegion = nil
		v.pendingRelease = false
		v.trackedPitch = -1
		v.crossfadeGain = 1
	}
	for k := range s.pitchTrack {
		s.pitchTrack[k].n = 0
	}
	s.ResetSequences()
}

// ResetSequences zeroes all per-key round-robin counters so sequences
// restart at position 1.
func (s *Sampler) ResetSequences() {
	for i := range s.seq {
		s.seq[i] = 0
	}
}

// SetRandSeed reseeds the note-on LCG for take variation. The default
// fixed seed renders deterministically; call this before playing to get
// a different (but still reproducible) random selection stream.
func (s *Sampler) SetRandSeed(seed uint64) {
	if seed == 0 {
		seed = defaultRandSeed
	}
	s.rand = seed
}

// nextRand draws one LCG step as a float32 in [0,1): the upper 24 bits
// of the state over 2^24. Integer arithmetic only; it never allocates
// and takes no locks.
func (s *Sampler) nextRand() float32 {
	s.rand = s.rand*6364136223846793005 + 1442695040888963407
	return float32(s.rand>>40) / 16777216
}

// randEligible reports whether the region's lorand/hirand window admits
// a roll. The default 0.0-1.0 window always passes.
func randEligible(rg *Region, roll float32) bool {
	return roll >= rg.LoRand && roll <= rg.HiRand
}

// Regions returns the loaded SFZ regions, nil before Load.
func (s *Sampler) Regions() []Region {
	if s.sfz == nil {
		return nil
	}
	return s.sfz.Regions
}

// SetParameters applies recognized parameters, clamped:
//
//	master_volume [0.0, 1.0]  linear master gain
//	attack        [0.0005, 2] voice envelope attack in seconds
//	release       [0.005, 10] voice envelope release in seconds
//
// Envelope changes take effect on the next NoteOn. Unknown keys are
// ignored.
func (s *Sampler) SetParameters(params map[string]float32) {
	if v, ok := params["master_volume"]; ok {
		s.masterGain = dsp.ClampF32(v, 0, 1)
	}
	if v, ok := params["attack"]; ok {
		s.attack = float64(dsp.ClampF32(v, 0.0005, 2))
	}
	if v, ok := params["release"]; ok {
		s.release = float64(dsp.ClampF32(v, 0.005, 10))
	}
}

// NoteOn triggers every region matching the pitch and velocity, one voice
// per region, allocating or stealing voices as needed. Matching runs in
// two stages: first one lorand/hirand roll per note-on filters the
// eligible set (regions whose window misses the roll stay silent), then
// round-robin sequence filtering cycles within that reduced set —
// random selection picks the subset, round-robin cycles inside it.
// Regions sharing a sequence identity cycle through their positions on
// successive triggers, driven by the pitch's own counter: the counter
// advances exactly once per note-on when a sequence group engages, so
// interleaved notes cycle independently.
func (s *Sampler) NoteOn(pitch int, velocity float32) {
	s.NoteOnEx(pitch, velocity, instruments.EnvelopeOverride{})
}

// NoteOnEx triggers every region matching the pitch and velocity like
// NoteOn, carrying per-note envelope time overrides with no vibrato
// and full expression.
func (s *Sampler) NoteOnEx(pitch int, velocity float32, env instruments.EnvelopeOverride) {
	s.NoteOnParams(pitch, velocity, instruments.NoteParams{Env: env, Expression: 1})
}

// NoteOnParams triggers every region matching the pitch and velocity
// like NoteOn, carrying the full per-note payload: envelope overrides,
// vibrato, and expression gain. Times without a Has flag fall back to
// the region or sampler default; sustain always comes from the region.
// Matching runs random filtering, then crossfade gain calculation, then
// sequence selection within each crossfade layer; the collected trigger
// set fires highest-gain-first with voice-pool overflow dropped.
func (s *Sampler) NoteOnParams(pitch int, velocity float32, params instruments.NoteParams) {
	if s.sfz == nil {
		return
	}
	if pitch < 0 || pitch > 127 {
		return
	}
	vel := dsp.ClampF32(velocity, 0, 1)
	velInt := int(vel * 127)
	if velInt > 127 {
		velInt = 127
	}
	if cap(s.trig) < len(s.sfz.Regions) {
		s.trig = make([]xfTrigger, 0, len(s.sfz.Regions))
	}
	s.trig = s.trig[:0]
	roll := s.nextRand()
	step := s.seq[pitch]
	advanced := false
	for i := range s.sfz.Regions {
		rg := &s.sfz.Regions[i]
		if !randEligible(rg, roll) || !rg.Matches(pitch, velInt) {
			continue
		}
		if rg.SeqLength <= 1 || seqMatched(s.sfz.Regions, i, pitch, velInt, roll) < 2 {
			// Legacy region or lone sequence member: play through.
			s.trig = append(s.trig, xfTrigger{rg: rg, gain: xfadeGain(rg, pitch, velInt)})
			continue
		}
		if seqServedBefore(s.sfz.Regions, i, pitch, velInt, roll) {
			continue // an earlier region already served this group
		}
		// Every member holding this step plays (one per crossfade
		// layer); single-member groups behave exactly as before.
		want := step%rg.SeqLength + 1
		for j := range s.sfz.Regions {
			rj := &s.sfz.Regions[j]
			if rj.Sample != nil && rj.SeqPosition == want && randEligible(rj, roll) && seqIdentity(rg, rj) {
				s.trig = append(s.trig, xfTrigger{rg: rj, gain: xfadeGain(rj, pitch, velInt)})
			}
		}
		advanced = true
	}
	if advanced {
		s.seq[pitch] = step + 1
	}
	s.fireTriggers(pitch, vel, params)
}

// xfTrigger is one collected voice request: the region to play and its
// equal-power crossfade gain at the trigger velocity and pitch.
type xfTrigger struct {
	rg   *Region
	gain float32
}

// xfadeGain returns the region's equal-power crossfade multiplier at
// the trigger pitch and velocity: the product of up to four zone
// factors (velocity/key fade-in/out). Inactive zones (equal bounds)
// contribute 1.0; progress clamps into its zone.
func xfadeGain(rg *Region, pitch, vel int) float32 {
	fadeIn := func(v, lo, hi int) float32 {
		if lo == hi {
			return 1
		}
		if lo > hi {
			lo, hi = hi, lo
		}
		t := float32(v-lo) / float32(hi-lo)
		if t < 0 {
			t = 0
		} else if t > 1 {
			t = 1
		}
		return float32(math.Sin(float64(t) * math.Pi / 2))
	}
	fadeOut := func(v, lo, hi int) float32 {
		if lo == hi {
			return 1
		}
		if lo > hi {
			lo, hi = hi, lo
		}
		t := float32(v-lo) / float32(hi-lo)
		if t < 0 {
			t = 0
		} else if t > 1 {
			t = 1
		}
		return float32(math.Cos(float64(t) * math.Pi / 2))
	}
	return fadeIn(vel, rg.XfinLoVel, rg.XfinHiVel) *
		fadeOut(vel, rg.XfoutLoVel, rg.XfoutHiVel) *
		fadeIn(pitch, rg.XfinLoKey, rg.XfinHiKey) *
		fadeOut(pitch, rg.XfoutLoKey, rg.XfoutHiKey)
}

// withXFade stamps a collected crossfade gain onto the payload.
func withXFade(params instruments.NoteParams, gain float32) instruments.NoteParams {
	params.XFadeGain = gain
	params.HasXFade = true
	return params
}

// fireTriggers sounds the collected trigger set. A lone trigger takes
// the legacy path (off_by, auto-fade, general steal). A crossfade set
// first applies off_by and auto-fade per member region, then starts the
// highest-gain members in free voices and drops the rest lowest-first;
// with no free voice the dominant member falls back to the legacy path
// so it is always heard. Order is stable for equal gains.
func (s *Sampler) fireTriggers(pitch int, vel float32, params instruments.NoteParams) {
	ts := s.trig
	if len(ts) == 0 {
		return
	}
	if len(ts) == 1 {
		s.trigger(ts[0].rg, pitch, vel, withXFade(params, ts[0].gain))
		return
	}
	for k := range ts {
		if ts[k].rg.OffBy != 0 {
			s.cutOffBy(ts[k].rg.OffBy)
		}
	}
	for k := range ts {
		dup := false
		for j := 0; j < k; j++ {
			if ts[j].rg == ts[k].rg {
				dup = true
				break
			}
		}
		if !dup {
			s.autoFadePitch(ts[k].rg, pitch)
		}
	}
	// Descending gain, stable: the dominant layer wins every tie.
	for a := 1; a < len(ts); a++ {
		for b := a; b > 0 && ts[b].gain > ts[b-1].gain; b-- {
			ts[b], ts[b-1] = ts[b-1], ts[b]
		}
	}
	var free [MaxVoices]int
	nf := 0
	for i := range s.voices {
		if !s.voices[i].Active {
			free[nf] = i
			nf++
		}
	}
	for k := range ts {
		p := withXFade(params, ts[k].gain)
		switch {
		case k < nf:
			s.startVoice(&s.voices[free[k]], ts[k].rg, pitch, vel, p)
		case k == 0:
			// Pool full: the dominant layer takes the legacy path.
			s.trigger(ts[k].rg, pitch, vel, p)
		default:
			// Dropped: lowest gains first.
		}
	}
}

// trigger starts one voice for rg, applying off_by cutoff and the
// per-pitch policy first. Same-pitch predecessors fade out over 10 ms
// (freed, never repurposed) so the new note never hard-cuts or choruses
// against itself. When a free voice exists it starts at once; otherwise
// the oldest voice fades out over stealFadeSamples frames before the
// new note begins. The voice reports the new pitch immediately so
// stealing stays observable, while the old audio keeps its velocity
// until the handoff.
func (s *Sampler) trigger(rg *Region, pitch int, vel float32, params instruments.NoteParams) {
	// off_by: starting this region fades the same exclusive group.
	if rg.OffBy != 0 {
		s.cutOffBy(rg.OffBy)
	}
	// Same-pitch predecessors fade first; general stealing only takes
	// voices from other pitches.
	s.autoFadePitch(rg, pitch)
	v := s.allocVoice()
	if v.Active {
		if !v.isStealing {
			v.isStealing = true
			v.stealFadeProgress = 0
			v.fadeStep = stealFadeStep
			v.releasing = false
			v.startTime = s.clock
		}
		v.stealFree = false
		v.pendingRegion = rg
		v.pendingPitch = pitch
		v.pendingVelocity = vel
		v.pendingRelease = false
		v.pendingParams = params
		v.Pitch = pitch
		v.stealSampleRate = s.sampleRate
		v.pendingAttack = s.attack
		v.pendingDecay = s.decay
		v.pendingSustain = s.sustain
		v.pendingReleaseEnv = s.release
		v.pendingStartTime = s.clock
		return
	}
	s.startVoice(v, rg, pitch, vel, params)
}

// fadeVoiceOut puts an active voice on the 10 ms fade-to-free path:
// the old note melts to silence and the voice frees instead of taking
// a pending note. Envelope and loop-crossfade state keep running
// underneath, multiplied by the fade gain.
func (s *Sampler) fadeVoiceOut(v *Voice) {
	v.isStealing = true
	v.stealFadeProgress = 0
	v.fadeStep = autoFadeStep
	v.stealFree = true
	v.releasing = false
	v.startTime = s.clock
}

// pitchCandidates gathers the active non-fading voices holding pitch,
// oldest first: the tracked roster, plus a full-scan fallback past the
// slot cap so no sounding voice hides. Stack-local, zero-alloc.
func (s *Sampler) pitchCandidates(pitch int) (cands [MaxVoices]*Voice, n int) {
	seen := func(vi *Voice) bool {
		for k := 0; k < n; k++ {
			if cands[k] == vi {
				return true
			}
		}
		return false
	}
	add := func(v *Voice) {
		if n < MaxVoices && !seen(v) {
			cands[n] = v
			n++
		}
	}
	if pitch >= 0 && pitch <= 127 {
		t := &s.pitchTrack[pitch]
		for k := 0; k < t.n; k++ {
			if v := t.voices[k]; v != nil && v.Active && !v.isStealing && v.Pitch == pitch {
				add(v)
			}
		}
	}
	for i := range s.voices {
		if v := &s.voices[i]; v.Active && !v.isStealing && v.Pitch == pitch {
			add(v)
		}
	}
	for a := 1; a < n; a++ {
		for b := a; b > 0 && cands[b].startTime < cands[b-1].startTime; b-- {
			cands[b], cands[b-1] = cands[b-1], cands[b]
		}
	}
	return cands, n
}

// autoFadePitch enforces the per-pitch policy before allocation. With a
// positive note_polyphony limit it fades the oldest voices past the
// limit (room for the incoming note included); with unlimited (0) it
// fades same-region doubles so one sample never choruses against
// itself, leaving other regions (layers, round-robin takes) to overlap.
func (s *Sampler) autoFadePitch(rg *Region, pitch int) {
	cands, n := s.pitchCandidates(pitch)
	if rg.NotePolyphony > 0 {
		for k := 0; k < n-rg.NotePolyphony+1; k++ {
			s.fadeVoiceOut(cands[k])
		}
		return
	}
	for k := 0; k < n; k++ {
		if cands[k].Region == rg {
			s.fadeVoiceOut(cands[k])
		}
	}
}

// seqIdentity reports whether a and b belong to the same round-robin
// sequence group: equal length and equal key range. Velocity ranges are
// deliberately excluded: velocity is the trigger-time filter dimension,
// and including it would keep partially matched groups (one member hit
// by this velocity, its partner by another) from ever engaging as one
// group, which would make single-match passthrough unobservable.
func seqIdentity(a, b *Region) bool {
	return a.SeqLength == b.SeqLength &&
		a.LoKey == b.LoKey && a.HiKey == b.HiKey
}

// seqMatched counts the regions matching pitch, velocity, and the roll
// that share region i's sequence identity, including region i itself.
// The scan uses only stack-local variables and allocates nothing.
func seqMatched(regions []Region, i, pitch, vel int, roll float32) int {
	n := 0
	for j := range regions {
		if randEligible(&regions[j], roll) && regions[j].Matches(pitch, vel) && seqIdentity(&regions[i], &regions[j]) {
			n++
		}
	}
	return n
}

// seqServedBefore reports whether an earlier eligible region shares
// region i's sequence identity, meaning the group was already served by
// this note-on and region i must stay silent.
func seqServedBefore(regions []Region, i, pitch, vel int, roll float32) bool {
	for j := 0; j < i; j++ {
		if randEligible(&regions[j], roll) && regions[j].Matches(pitch, vel) && seqIdentity(&regions[i], &regions[j]) {
			return true
		}
	}
	return false
}

// cutOffBy fades every sounding voice in the given off group over the
// 10 ms auto-fade path instead of hard-silencing it. Voices mid-steal
// already fade out and are left alone. Cut voices keep the releasing
// mark so steal preference and the existing cutoff tests observe them.
func (s *Sampler) cutOffBy(group int) {
	for i := range s.voices {
		v := &s.voices[i]
		if v.Active && !v.releasing && !v.isStealing && v.Region != nil && v.Region.OffBy == group {
			s.fadeVoiceOut(v)
			v.Envelope.Release()
			v.releasing = true
			v.ReleaseTime = s.clock
		}
	}
}

// NoteOff releases all voices holding the pitch. one_shot regions ignore
// note-off and play to the sample end. A note-off for a stealing voice's
// new pitch is stored and applied after the fade; the fading old note
// never responds to note-off.
func (s *Sampler) NoteOff(pitch int) {
	for i := range s.voices {
		v := &s.voices[i]
		if !v.Active {
			continue
		}
		if v.isStealing {
			if pitch == v.Pitch {
				v.pendingRelease = true
			}
			continue
		}
		if !v.releasing && v.Pitch == pitch && v.Region.LoopMode != LoopOneShot {
			v.Envelope.Release()
			v.releasing = true
			v.ReleaseTime = s.clock
		}
	}
}

// Process renders all active voices into buffer, advancing deltaTime
// seconds per frame. Each voice is mono; it is mixed in with its region
// gain, master gain, and constant-power pan.
func (s *Sampler) Process(buffer *dsp.Buffer, deltaTime float64) {
	if buffer == nil || deltaTime <= 0 {
		return
	}
	n := buffer.Len()
	if n == 0 {
		return
	}
	if len(s.mix) < n {
		s.mix = make([]float32, n) // one-off grow to the largest block seen
	}
	left, right := buffer.Left, buffer.Right
	clear(left)
	clear(right)
	blockTime := deltaTime * float64(n)
	for i := range s.voices {
		v := &s.voices[i]
		if !v.Active {
			continue
		}
		v.Process(s.mix[:n], blockTime)
		g := v.gain * s.masterGain
		gl, gr := g*v.panL, g*v.panR
		for j := 0; j < n; j++ {
			left[j] += s.mix[j] * gl
			right[j] += s.mix[j] * gr
		}
	}
	s.clock += blockTime
}

// ActiveVoices reports how many voices are currently sounding.
func (s *Sampler) ActiveVoices() int {
	count := 0
	for i := range s.voices {
		if s.voices[i].Active {
			count++
		}
	}
	return count
}

// allocVoice picks a voice for a new note: a free one, else the oldest
// voice in release stage, else the oldest sounding voice.
func (s *Sampler) allocVoice() *Voice {
	for i := range s.voices {
		if !s.voices[i].Active {
			return &s.voices[i]
		}
	}
	var oldest *Voice
	for i := range s.voices {
		v := &s.voices[i]
		if v.releasing && (oldest == nil || v.ReleaseTime < oldest.ReleaseTime) {
			oldest = v
		}
	}
	if oldest != nil {
		return oldest
	}
	for i := range s.voices {
		v := &s.voices[i]
		if oldest == nil || v.startTime < oldest.startTime {
			oldest = v
		}
	}
	return oldest
}

// startVoice (re)initializes a voice for a region at the given pitch,
// applying the region's sample window, transpose/tune, amp envelope,
// vibrato, and expression from the per-note payload.
func (s *Sampler) startVoice(v *Voice, rg *Region, pitch int, velocity float32, params instruments.NoteParams) {
	initVoice(v, rg, pitch, velocity, s.sampleRate,
		s.attack, s.decay, s.sustain, s.release, s.clock, params)
}

// initVoice (re)initializes v for rg without touching the sampler: the
// shared core behind startVoice and the steal handoff. It clears loop
// crossfade and steal state, derives the sample window, playback rate,
// gain, pan, loop points, envelope, vibrato, and expression, then
// triggers the envelope. Per-note overrides replace individual
// attack/decay/release times; sustain always comes from the region.
// Vibrato converts depth semitones to a fractional rate swing once, and
// the LFO phase and engagement timer restart at zero. It allocates
// nothing.
func initVoice(v *Voice, rg *Region, pitch int, velocity float32, sampleRate, attack, decay, sustain, release, startTime float64, params instruments.NoteParams) {
	sd := rg.Sample
	v.Region = rg
	v.Pitch = pitch
	v.Velocity = velocity
	v.PlaybackRate = rg.PlaybackRateFor(pitch, sampleRate)

	// Sample window: offset start and optional exclusive end frame.
	end := float64(sd.Frames())
	if rg.End > 0 && float64(rg.End) < end {
		end = float64(rg.End)
	}
	v.end = end
	v.Phase = 0
	if rg.Offset > 0 && float64(rg.Offset) < end {
		v.Phase = float64(rg.Offset)
	}

	v.Active = true
	v.releasing = false
	v.startTime = startTime
	v.ReleaseTime = 0
	v.gain = rg.LinearGain()

	// Constant-power pan (sqrt law): unity at the hard sides, -3 dB at
	// center, and exact zeros at the extremes.
	pan := clampF(float64(rg.Pan), -1, 1)
	v.panL = float32(math.Sqrt((1 - pan) / 2))
	v.panR = float32(math.Sqrt((1 + pan) / 2))

	v.looping = false
	v.loopStart, v.loopEnd = 0, end
	if rg.LoopMode == LoopContinuous && sd.LoopEnd > sd.LoopStart+1 && sd.LoopEnd <= sd.Frames() {
		v.looping = true
		v.loopStart = float64(sd.LoopStart)
		v.loopEnd = float64(sd.LoopEnd)
		if v.loopEnd > end {
			v.loopEnd = end
			if v.loopStart >= v.loopEnd {
				v.looping = false
			}
		}
	}

	// Fresh notes play full crossfade gain unless the trigger set
	// resolved a quieter layer blend.
	v.crossfadeGain = 1
	if params.HasXFade {
		v.crossfadeGain = params.XFadeGain
	}
	// Fresh notes start outside the crossfade with no steal pending.
	v.inCrossfade = false
	v.crossfadePhase = 0
	v.crossfadeReadPos = 0
	v.isStealing = false
	v.stealFadeProgress = 0
	v.fadeStep = stealFadeStep
	v.stealFree = false
	v.pendingPitch = 0
	v.pendingVelocity = 0
	v.pendingRegion = nil
	v.pendingRelease = false
	v.pendingParams = instruments.NoteParams{}
	v.stealSampleRate = sampleRate
	v.pendingAttack = attack
	v.pendingDecay = decay
	v.pendingSustain = sustain
	v.pendingReleaseEnv = release
	v.pendingStartTime = startTime

	// Vibrato restarts per note: depth semitones become a fractional
	// rate swing, phase and engagement timer reset to zero.
	env := params.Env
	v.hasVibrato = params.HasVibrato && params.VibratoRate > 0 && params.VibratoDepth > 0
	v.vibRate = params.VibratoRate
	v.vibDepthM = 0
	if v.hasVibrato {
		v.vibDepthM = math.Pow(2, params.VibratoDepth/12) - 1
	}
	v.vibPhase = 0
	v.vibTimer = 0

	// Expression scales the sustained output; unset plays full volume,
	// NaN keeps full volume, and negatives clamp to silence.
	expr := 1.0
	if params.HasExpression {
		expr = float64(params.Expression)
		if math.IsNaN(expr) {
			expr = 1
		} else if expr < 0 {
			expr = 0
		} else if expr > 1 {
			expr = 1
		}
	}
	v.exprGain = float32(expr)

	if rg.HasAmpEG {
		eg := rg.AmpEG
		attack = float64(eg.Attack)
		decay = float64(eg.Decay)
		sustain = clampF(float64(eg.Sustain)/100, 0, 1)
		release = float64(eg.Release)
	}
	// Per-note overrides win over the region or sampler default. Only
	// positive times apply: absent (zero) or negative values keep the
	// base, and the envelope clamps tiny positives to its minimum.
	if env.HasAttack && env.Attack > 0 {
		attack = env.Attack
	}
	if env.HasDecay && env.Decay > 0 {
		decay = env.Decay
	}
	if env.HasRelease && env.Release > 0 {
		release = env.Release
	}
	v.Envelope.SetParameters(attack, decay, sustain, release)
	v.Envelope.Trigger()
	v.trackRegister(pitch)
}
