// Package engine wires scores to instruments and the mixer: it builds a
// playable graph from a score.Score and provides block processing for
// headless offline WAV rendering.
package engine

import (
	"fmt"
	"math"
	"sort"
	"strings"

	"resonata/pkg/dsp"
	"resonata/pkg/instruments"
	"resonata/pkg/instruments/ocarina"
	"resonata/pkg/instruments/sampler"
	"resonata/pkg/mixer"
	"resonata/pkg/score"
)

// Defaults for the application transport and renderers.
const (
	DefaultSampleRate = 48000
	DefaultBlockSize  = 1024

	// tailSeconds extends playback past the last note-off so envelopes
	// and the FDN reverb tail decay inside the render (hall RT60 ~2.3 s).
	tailSeconds = 2.5

	// DefaultReverbWet is the master reverb mix for score renders.
	DefaultReverbWet = 0.2
)

// noteEvent fires NoteOn/NoteOff on one track's instrument at a frame.
// On-events carry the score note's full per-note payload (envelope
// overrides, vibrato, expression); off-events ignore it.
type noteEvent struct {
	frame  int
	track  int
	pitch  int
	vel    float32
	on     bool
	params instruments.NoteParams
}

// Engine plays a score: one instrument per track, summed through a
// Mixer. It is not safe for concurrent use; callers serialize
// access to a single engine instance.
type Engine struct {
	score       *score.Score
	sampleRate  float64
	blockSize   int
	insts       []instruments.Instrument
	mix         *mixer.Mixer
	events      []noteEvent
	nextEvent   int
	frame       int
	totalFrames int

	// Meter state for the last processed block.
	peakL, peakR float32
	trackPeak    []float32
}

// New builds the engine for a validated score at sampleRate Hz. Each
// track gets its instrument: "ocarina" (physical model) or "sampler"
// (SFZ file resolved relative to the working directory). Every note is
// scheduled as frame-accurate on/off events; staccato articulation
// releases at half the written duration, others hold the full length.
func New(s *score.Score, sampleRate float64, blockSize int) (*Engine, error) {
	if err := score.Validate(s); err != nil {
		return nil, fmt.Errorf("invalid score: %w", err)
	}
	if sampleRate <= 0 {
		sampleRate = DefaultSampleRate
	}
	if blockSize <= 1 {
		blockSize = DefaultBlockSize
	}

	e := &Engine{
		score:      s,
		sampleRate: sampleRate,
		blockSize:  blockSize,
		insts:      make([]instruments.Instrument, len(s.Tracks)),
		trackPeak:  make([]float32, len(s.Tracks)),
	}
	e.mix = mixer.New(int(sampleRate), len(s.Tracks), blockSize)
	e.mix.SetReverbWet(DefaultReverbWet)
	for i := range s.Tracks {
		tr := &s.Tracks[i]
		inst, err := newInstrument(tr.Instrument, sampleRate)
		if err != nil {
			return nil, fmt.Errorf("track %d (%s): %w", i, tr.ID, err)
		}
		if trackHasEQ(tr.EQ) {
			spec := EQSpecFor(tr.EQ)
			inst = &eqInstrument{
				inner: inst,
				eqL:   dsp.NewEQ(sampleRate, spec),
				eqR:   dsp.NewEQ(sampleRate, spec),
			}
		}
		e.insts[i] = inst
		idx := e.mix.AddTrack(inst, tr.Pan, tr.Volume)
		if idx < 0 {
			return nil, fmt.Errorf("track %d (%s): mixer is full", i, tr.ID)
		}
		e.mix.SetSend(idx, tr.ReverbSend)
		if tr.Delay != nil {
			// The deprecated delay.send field parses for backward
			// compatibility but has no effect: each echo processes
			// its track's full bus signal internally.
			e.mix.SetTrackDelay(idx, delayConfigFor(tr.Delay), s.Metadata.BPM)
		}
	}
	e.assignRooms(s)

	for i := range s.Tracks {
		for j, t := range breathTimes(&s.Tracks[i], s.Metadata.BreathMs) {
			n := s.Tracks[i].Notes[j]
			on := int(math.Round(t * sampleRate))
			hold := n.Duration
			if strings.EqualFold(n.Articulation.Type, "staccato") {
				hold *= 0.5
			}
			off := int(math.Round((t + hold) * sampleRate))
			if off <= on {
				off = on + 1
			}
			e.events = append(e.events,
				noteEvent{frame: on, track: i, pitch: n.Pitch, vel: n.Velocity, on: true, params: paramsFor(n, &s.Tracks[i])},
				noteEvent{frame: off, track: i, pitch: n.Pitch, on: false})
		}
	}
	sort.SliceStable(e.events, func(a, b int) bool {
		if e.events[a].frame != e.events[b].frame {
			return e.events[a].frame < e.events[b].frame
		}
		return !e.events[a].on && e.events[b].on // note-offs first on shared frames
	})
	e.totalFrames = int(math.Round(s.Duration()*sampleRate)) + int(tailSeconds*sampleRate)
	return e, nil
}

// newInstrument instantiates the instrument for a track definition and
// applies its score parameters.
func newInstrument(def score.InstrumentDef, sampleRate float64) (instruments.Instrument, error) {
	switch strings.ToLower(def.Type) {
	case "ocarina":
		o := ocarina.New(sampleRate)
		o.SetParameters(def.Parameters)
		return o, nil
	case "sampler":
		if strings.TrimSpace(def.File) == "" {
			return nil, fmt.Errorf("sampler instrument needs a file path")
		}
		sm, err := sampler.LoadFile(def.File, sampleRate)
		if err != nil {
			return nil, err
		}
		sm.SetParameters(def.Parameters)
		return sm, nil
	}
	return nil, fmt.Errorf("unknown instrument type %q (want ocarina or sampler)", def.Type)
}

// trackHasEQ reports whether the score track requests any EQ stage.
func trackHasEQ(eq score.TrackEQ) bool {
	return eq.HPF > 0 || eq.LowShelf.Freq > 0 || eq.MidPeak.Freq > 0 || eq.HighShelf.Freq > 0
}

// EQSpecFor maps a score track's EQ DSL onto the dsp spec.
func EQSpecFor(eq score.TrackEQ) dsp.EQSpec {
	band := func(b score.EQBand) dsp.EQBandSpec {
		return dsp.EQBandSpec{Freq: b.Freq, GainDB: b.Gain, Q: b.Q}
	}
	return dsp.EQSpec{
		HPF:       eq.HPF,
		LowShelf:  band(eq.LowShelf),
		MidPeak:   band(eq.MidPeak),
		HighShelf: band(eq.HighShelf),
	}
}

// overrideFor maps a score note's envelope overrides onto the
// instrument override: only positive times count as present, so absent
// (zero) or negative values keep the instrument or SFZ default. It
// allocates nothing.
func overrideFor(n score.NoteEvent) instruments.EnvelopeOverride {
	return instruments.EnvelopeOverride{
		Attack:     n.AttackSec,
		Decay:      n.DecaySec,
		Release:    n.ReleaseSec,
		HasAttack:  n.AttackSec > 0,
		HasDecay:   n.DecaySec > 0,
		HasRelease: n.ReleaseSec > 0,
	}
}

// Default score vibrato when the object sets no usable sub-field.
const (
	defaultVibRate  = 5.0
	defaultVibDepth = 0.03
)

// paramsFor resolves a score note plus its track into the full
// per-note payload: envelope overrides, vibrato (note wins over
// track), and expression (note wins over track, default full volume).
// It allocates nothing.
func paramsFor(n score.NoteEvent, tr *score.Track) instruments.NoteParams {
	p := instruments.NoteParams{Env: overrideFor(n)}
	vib := tr.Vibrato
	if n.Vibrato != nil {
		vib = n.Vibrato
	}
	if vib != nil {
		rate, depth := vib.Rate, vib.Depth
		if !(rate > 0) {
			rate = defaultVibRate
		}
		if !(depth > 0) {
			depth = defaultVibDepth
		}
		p.HasVibrato = true
		p.VibratoRate = rate
		p.VibratoDepth = depth
	}
	expr := tr.Expression
	if n.Expression != nil {
		expr = n.Expression
	}
	p.Expression, p.HasExpression = 1, true
	if expr != nil {
		v := *expr
		if math.IsNaN(v) {
			v = 1
		} else if v < 0 {
			v = 0
		} else if v > 1 {
			v = 1
		}
		p.Expression = float32(v)
	}
	return p
}

// delayConfigFor maps a score track's echo DSL onto the dsp spec. JSON
// values are authoritative: wet zero renders dry. An empty subdivision
// defaults to a quarter note; damping at zero selects the dsp default.
func delayConfigFor(td *score.TrackDelay) dsp.DelayConfig {
	mode := dsp.DelayModeStereo
	switch strings.ToLower(strings.TrimSpace(td.Mode)) {
	case "pingpong", "ping-pong", "ping_pong":
		mode = dsp.DelayModePingPong
	}
	sub := dsp.SubdivisionQuarter
	if name := strings.TrimSpace(td.Subdivision); name != "" {
		if parsed, ok := dsp.ParseSubdivision(name); ok {
			sub = parsed
		}
	}
	return dsp.DelayConfig{
		Mode:         mode,
		Subdivision:  sub,
		DelaySeconds: td.Seconds,
		Feedback:     td.Feedback,
		DampingHz:    td.DampingHz,
		Wet:          td.Wet,
	}
}

// eqInstrument decorates an instrument with a per-track EQ chain; one
// dsp.EQ per channel keeps the biquad states independent. The sculpt
// sits before the mixer, so track sends carry the EQ'd signal.
type eqInstrument struct {
	inner instruments.Instrument
	eqL   *dsp.EQ
	eqR   *dsp.EQ
}

func (e *eqInstrument) Process(buffer *dsp.Buffer, deltaTime float64) {
	e.inner.Process(buffer, deltaTime)
	e.eqL.ProcessInPlace(buffer.Left)
	e.eqR.ProcessInPlace(buffer.Right)
}

func (e *eqInstrument) NoteOn(pitch int, velocity float32) { e.inner.NoteOn(pitch, velocity) }
func (e *eqInstrument) NoteOff(pitch int)                  { e.inner.NoteOff(pitch) }

// NoteOnParams forwards the full per-note payload to the wrapped
// instrument when it accepts it, falling back to a plain NoteOn.
func (e *eqInstrument) NoteOnParams(pitch int, velocity float32, params instruments.NoteParams) {
	if ex, ok := e.inner.(instruments.ParamsVoicer); ok {
		ex.NoteOnParams(pitch, velocity, params)
		return
	}
	e.inner.NoteOn(pitch, velocity)
}

// NoteOnEx forwards per-note envelope overrides to the wrapped
// instrument when it accepts them, falling back to a plain NoteOn.
func (e *eqInstrument) NoteOnEx(pitch int, velocity float32, env instruments.EnvelopeOverride) {
	if ex, ok := e.inner.(instruments.EnvelopeVoicer); ok {
		ex.NoteOnEx(pitch, velocity, env)
		return
	}
	e.inner.NoteOn(pitch, velocity)
}
func (e *eqInstrument) SetParameters(params map[string]float32) {
	e.inner.SetParameters(params)
}

// Score returns the loaded score.
func (e *Engine) Score() *score.Score { return e.score }

// SampleRate reports the render rate in Hz.
func (e *Engine) SampleRate() float64 { return e.sampleRate }

// BlockSize is the default processing block in frames.
func (e *Engine) BlockSize() int { return e.blockSize }

// Mixer exposes the mix graph for wet/tune adjustments.
func (e *Engine) Mixer() *mixer.Mixer { return e.mix }

// Duration is the full play length in seconds, tail included.
func (e *Engine) Duration() float64 { return float64(e.totalFrames) / e.sampleRate }

// Position is the playhead in seconds.
func (e *Engine) Position() float64 { return float64(e.frame) / e.sampleRate }

// PositionFrames is the playhead in frames.
func (e *Engine) PositionFrames() int { return e.frame }

// TotalFrames is the full play length in frames.
func (e *Engine) TotalFrames() int { return e.totalFrames }

// TrackCount returns the number of score tracks (mixer strips).
func (e *Engine) TrackCount() int { return len(e.insts) }

// Peak returns the master left/right peaks of the last processed block.
func (e *Engine) Peak() (float32, float32) { return e.peakL, e.peakR }

// TrackPeak returns track i's peak over the last processed block.
func (e *Engine) TrackPeak(i int) float32 {
	if i < 0 || i >= len(e.trackPeak) {
		return 0
	}
	return e.trackPeak[i]
}

// ProcessBlock renders one blockSize block, updating meters. The audio
// is available via Mixer().Master() until the next call.
func (e *Engine) ProcessBlock() {
	e.ProcessFrames(e.blockSize, nil)
}

// ProcessFrames renders exactly n frames in sample-accurate sub-blocks
// (splitting at note-event boundaries) and calls sink with each rendered
// chunk of interleaved stereo audio. sink may be nil. The chunk slice is
// only valid until the next call.
func (e *Engine) ProcessFrames(n int, sink func(master []float32)) {
	e.peakL, e.peakR = 0, 0
	for i := range e.trackPeak {
		e.trackPeak[i] = 0
	}
	rendered := 0
	for rendered < n {
		// Fire every event due at the current playhead.
		for e.nextEvent < len(e.events) && e.events[e.nextEvent].frame <= e.frame {
			ev := e.events[e.nextEvent]
			if ev.on {
				if ex, ok := e.insts[ev.track].(instruments.ParamsVoicer); ok {
					ex.NoteOnParams(ev.pitch, ev.vel, ev.params)
				} else {
					e.insts[ev.track].NoteOn(ev.pitch, ev.vel)
				}
			} else {
				e.insts[ev.track].NoteOff(ev.pitch)
			}
			e.nextEvent++
		}
		chunk := n - rendered
		if e.nextEvent < len(e.events) {
			if gap := e.events[e.nextEvent].frame - e.frame; gap < chunk {
				chunk = gap // split so the event lands exactly
			}
		}
		if chunk < 1 {
			chunk = 1
		}
		e.mix.Process(chunk)
		e.updateMeters()
		if sink != nil {
			sink(e.mix.Master())
		}
		e.frame += chunk
		rendered += chunk
	}
}

// updateMeters folds the just-rendered block into the peak statistics.
func (e *Engine) updateMeters() {
	master := e.mix.Master()
	for i := 0; i+1 < len(master); i += 2 {
		if a := abs32(master[i]); a > e.peakL {
			e.peakL = a
		}
		if a := abs32(master[i+1]); a > e.peakR {
			e.peakR = a
		}
	}
	for i := range e.trackPeak {
		if p := e.mix.TrackPeak(i); p > e.trackPeak[i] {
			e.trackPeak[i] = p
		}
	}
}

// abs32 returns the magnitude of v.
func abs32(v float32) float32 {
	if v < 0 {
		return -v
	}
	return v
}
