package score

import (
	"errors"
	"fmt"
	"math"
	"strconv"
	"strings"
)

// KnownArticulations lists the articulation types accepted by Validate.
var KnownArticulations = []string{"staccato", "legato", "tenuto"}

// Range limits enforced by Validate.
const (
	MinMIDIPitch = 0
	MaxMIDIPitch = 127
	MinBPM       = 0    // exclusive
	MaxBPM       = 1000 // inclusive
)

// Validate checks required fields and value ranges across the whole
// score. All violations are collected and joined into a single error;
// the result is nil when the score is valid.
func Validate(s *Score) error {
	if s == nil {
		return errors.New("score is nil")
	}
	var errs []error
	errs = append(errs, validateMetadata(s.Metadata)...)
	if len(s.Tracks) == 0 {
		errs = append(errs, errors.New("score must contain at least one track"))
	}
	seen := make(map[string]int, len(s.Tracks))
	for i := range s.Tracks {
		errs = append(errs, validateTrack(&s.Tracks[i], i, s.Metadata.Transpose, seen)...)
	}
	return errors.Join(errs...)
}

// validateMetadata checks the global score metadata.
func validateMetadata(m Metadata) []error {
	var errs []error
	if strings.TrimSpace(m.Title) == "" {
		errs = append(errs, errors.New("metadata.title is required"))
	}
	if !(m.BPM > MinBPM) || m.BPM > MaxBPM || math.IsNaN(m.BPM) {
		errs = append(errs, fmt.Errorf("metadata.bpm must be in (%g, %g], got %g", float64(MinBPM), float64(MaxBPM), m.BPM))
	}
	if err := validateTimeSignature(m.TimeSignature); err != nil {
		errs = append(errs, err)
	}
	if !(m.Swing >= 0) || m.Swing > 1 {
		errs = append(errs, fmt.Errorf("metadata.swing must be in [0, 1], got %g", m.Swing))
	}
	if !(m.BreathMs >= 0) || math.IsInf(m.BreathMs, 0) || math.IsNaN(m.BreathMs) {
		errs = append(errs, fmt.Errorf("metadata.breath must be a finite value >= 0 ms, got %g", m.BreathMs))
	}
	return errs
}

// validateTimeSignature requires "N/D" with positive integers, e.g. "4/4".
func validateTimeSignature(ts string) error {
	num, den, ok := strings.Cut(ts, "/")
	if ok {
		n, err1 := strconv.Atoi(strings.TrimSpace(num))
		d, err2 := strconv.Atoi(strings.TrimSpace(den))
		if err1 == nil && err2 == nil && n > 0 && d > 0 {
			return nil
		}
	}
	return fmt.Errorf("metadata.time_signature must be \"N/D\" with positive integers, got %q", ts)
}

// validateTrack checks one track and its notes. seen maps track IDs to
// their first index for duplicate detection. globalTranspose is the
// metadata transpose, combined with the track transpose for
// transposition-aware pitch errors.
func validateTrack(t *Track, index int, globalTranspose int, seen map[string]int) []error {
	var errs []error
	label := fmt.Sprintf("tracks[%d] %q", index, t.ID)

	if strings.TrimSpace(t.ID) == "" {
		errs = append(errs, fmt.Errorf("tracks[%d]: id is required", index))
	} else if prev, dup := seen[t.ID]; dup {
		errs = append(errs, fmt.Errorf("%s: duplicate track id (first used by tracks[%d])", label, prev))
	} else {
		seen[t.ID] = index
	}

	if strings.TrimSpace(t.Instrument.Type) == "" {
		errs = append(errs, fmt.Errorf("%s: instrument.type is required", label))
	}
	if strings.EqualFold(t.Instrument.Type, "sampler") && strings.TrimSpace(t.Instrument.File) == "" {
		errs = append(errs, fmt.Errorf("%s: instrument.file is required for sampler instruments", label))
	}

	if t.Pan < -1 || t.Pan > 1 {
		errs = append(errs, fmt.Errorf("%s: pan must be in [-1, 1], got %g", label, t.Pan))
	}
	if t.Volume < 0 || t.Volume > 1 {
		errs = append(errs, fmt.Errorf("%s: volume must be in [0, 1], got %g", label, t.Volume))
	}
	if t.ReverbSend < 0 || t.ReverbSend > 1 {
		errs = append(errs, fmt.Errorf("%s: reverb_send must be in [0, 1], got %g", label, t.ReverbSend))
	}
	if !(t.Swing >= 0) || t.Swing > 1 {
		errs = append(errs, fmt.Errorf("%s: swing must be in [0, 1], got %g", label, t.Swing))
	}
	if !(t.BreathMs >= 0) || math.IsInf(t.BreathMs, 0) || math.IsNaN(t.BreathMs) {
		errs = append(errs, fmt.Errorf("%s: breath must be a finite value >= 0 ms, got %g", label, t.BreathMs))
	}
	errs = append(errs, validateRoom(t.Room, label)...)
	errs = append(errs, validateEQ(t.EQ, label)...)
	errs = append(errs, validateDelay(t.Delay, label)...)

	for j := range t.Notes {
		if err := checkTransposedPitch(&t.Notes[j], t, globalTranspose, index, j); err != nil {
			// Transposition pushed a valid pitch out of range: the
			// detailed error replaces the generic pitch-range error
			// but the remaining note checks still run.
			errs = append(errs, err)
			errs = append(errs, validateNote(&t.Notes[j], label, j, true)...)
			continue
		}
		errs = append(errs, validateNote(&t.Notes[j], label, j, false)...)
	}
	return errs
}

// checkTransposedPitch reports a detailed error when transposition pushed
// an originally valid pitch outside MIDI range, identifying the track,
// note index, original and resulting pitches, and both shifts. It returns
// nil when no transpose-specific diagnostic applies: no shift, a valid
// result, or an originally invalid pitch (covered by the generic pitch
// error). All notes are checked by the caller, so every offender is
// reported in one pass.
func checkTransposedPitch(n *NoteEvent, t *Track, globalTranspose, trackIndex, noteIndex int) error {
	shift := globalTranspose + t.Transpose
	if shift == 0 {
		return nil
	}
	if n.Pitch >= MinMIDIPitch && n.Pitch <= MaxMIDIPitch {
		return nil
	}
	original := n.Pitch - shift
	if original < MinMIDIPitch || original > MaxMIDIPitch {
		return nil
	}
	name := strings.TrimSpace(t.Name)
	if name == "" {
		name = strings.TrimSpace(t.ID)
	}
	if name == "" {
		name = fmt.Sprintf("tracks[%d]", trackIndex)
	}
	if n.Pitch > MaxMIDIPitch {
		return fmt.Errorf("track %s, note index %d, original pitch %d, global transpose %+d, track transpose %+d, resulting pitch %d exceeds maximum %d",
			name, noteIndex, original, globalTranspose, t.Transpose, n.Pitch, MaxMIDIPitch)
	}
	return fmt.Errorf("track %s, note index %d, original pitch %d, global transpose %+d, track transpose %+d, resulting pitch %d below minimum %d",
		name, noteIndex, original, globalTranspose, t.Transpose, n.Pitch, MinMIDIPitch)
}

// validateNote checks the timing, pitch, velocity, and articulation of a
// single note. When skipPitch is true the MIDI range check is omitted
// because a transposition error already covers it.
func validateNote(n *NoteEvent, trackLabel string, index int, skipPitch bool) []error {
	var errs []error
	at := func(format string, args ...interface{}) {
		errs = append(errs, fmt.Errorf("%s: notes[%d]: %s", trackLabel, index, fmt.Sprintf(format, args...)))
	}

	// !(x >= 0) also rejects NaN.
	if !(n.Time >= 0) || math.IsInf(n.Time, 0) {
		at("time must be a finite value >= 0, got %g", n.Time)
	}
	if !(n.Duration > 0) || math.IsInf(n.Duration, 0) {
		at("duration must be a finite value > 0, got %g", n.Duration)
	}
	if n.Pitch < MinMIDIPitch || n.Pitch > MaxMIDIPitch {
		if !skipPitch {
			at("pitch must be a MIDI note in [%d, %d], got %d", MinMIDIPitch, MaxMIDIPitch, n.Pitch)
		}
	}
	if !(n.Velocity >= 0) || n.Velocity > 1 {
		at("velocity must be in [0, 1], got %g", n.Velocity)
	}
	if n.Articulation.Type != "" && !isKnownArticulation(n.Articulation.Type) {
		at("unknown articulation type %q (known: %s)", n.Articulation.Type, strings.Join(KnownArticulations, ", "))
	}
	return errs
}

// validateEQ checks the per-track sculpt: frequencies must be positive
// and sub-ultrasonic, gains and Q within sane studio ranges. A band with
// all-zero fields is treated as absent.
func validateEQ(eq TrackEQ, label string) []error {
	var errs []error
	if eq.HPF < 0 || eq.HPF >= 100000 {
		errs = append(errs, fmt.Errorf("%s: eq.hpf must be 0 (off) or in (0, 100000) Hz, got %g", label, eq.HPF))
	}
	band := func(name string, b EQBand) {
		if b.Freq == 0 && b.Gain == 0 && b.Q == 0 {
			return // absent band
		}
		if b.Freq <= 0 || b.Freq >= 100000 {
			errs = append(errs, fmt.Errorf("%s: %s.freq must be in (0, 100000) Hz, got %g", label, name, b.Freq))
		}
		if b.Gain < -36 || b.Gain > 36 {
			errs = append(errs, fmt.Errorf("%s: %s.gain must be in [-36, 36] dB, got %g", label, name, b.Gain))
		}
		if b.Q < 0 || b.Q > 50 {
			errs = append(errs, fmt.Errorf("%s: %s.q must be in [0, 50], got %g", label, name, b.Q))
		}
	}
	band("eq_low", eq.LowShelf)
	band("eq_mid", eq.MidPeak)
	band("eq_high", eq.HighShelf)
	return errs
}

// validateDelay checks the per-track echo configuration. A nil delay
// disables the send. Mode must be stereo or pingpong, subdivisions follow
// the musical vocabulary, and feedback, wet, and send stay in range.
func validateDelay(d *TrackDelay, label string) []error {
	if d == nil {
		return nil
	}
	var errs []error
	switch strings.ToLower(strings.TrimSpace(d.Mode)) {
	case "", "stereo", "pingpong", "ping-pong", "ping_pong":
	default:
		errs = append(errs, fmt.Errorf("%s: delay.mode must be \"stereo\" or \"pingpong\", got %q", label, d.Mode))
	}
	if strings.TrimSpace(d.Subdivision) != "" {
		switch strings.ToLower(strings.TrimSpace(d.Subdivision)) {
		case "1/1", "1/2", "1/4", "1/8", "1/8d", "1/16", "1/16t",
			"whole", "half", "quarter", "eighth", "sixteenth":
		default:
			errs = append(errs, fmt.Errorf("%s: delay.subdivision must be one of 1/1 1/2 1/4 1/8 1/8d 1/16 1/16t, got %q", label, d.Subdivision))
		}
	}
	if !(d.Seconds >= 0) || math.IsInf(d.Seconds, 0) || d.Seconds > 4 {
		errs = append(errs, fmt.Errorf("%s: delay.seconds must be in [0, 4], got %g", label, d.Seconds))
	}
	if !(d.Feedback >= 0) || d.Feedback > 0.98 {
		errs = append(errs, fmt.Errorf("%s: delay.feedback must be in [0, 0.98], got %g", label, d.Feedback))
	}
	if d.DampingHz < 0 || d.DampingHz >= 100000 {
		errs = append(errs, fmt.Errorf("%s: delay.damping_hz must be in [0, 100000), got %g", label, d.DampingHz))
	}
	if !(d.Wet >= 0) || d.Wet > 1 {
		errs = append(errs, fmt.Errorf("%s: delay.wet must be in [0, 1], got %g", label, d.Wet))
	}
	if !(d.Send >= 0) || d.Send > 1 {
		errs = append(errs, fmt.Errorf("%s: delay.send must be in [0, 1], got %g", label, d.Send))
	}
	return errs
}

// validateRoom checks a track's acoustic space: preset strings must
// name cathedral, hall, room, or none; object fields stay in [0, 1].
// A nil room uses the master reverb and needs no check.
func validateRoom(r *Room, label string) []error {
	if r == nil {
		return nil
	}
	if r.IsPreset {
		if !IsRoomPreset(r.Preset) {
			return []error{fmt.Errorf("%s: room must be \"cathedral\", \"hall\", \"room\", or \"none\", got %q", label, r.Preset)}
		}
		return nil
	}
	var errs []error
	check := func(name string, v float64) {
		if !(v >= 0) || v > 1 {
			errs = append(errs, fmt.Errorf("%s: room.%s must be in [0, 1], got %g", label, name, v))
		}
	}
	check("size", r.Config.Size)
	check("damping", r.Config.Damping)
	check("width", r.Config.Width)
	return errs
}

// Warnings collects non-fatal score notices: suspicious but legal
// configurations that render exactly as written. A nil score yields nil.
// Warnings never block rendering; see Validate for hard errors.
func Warnings(s *Score) []string {
	if s == nil {
		return nil
	}
	var out []string
	for i := range s.Tracks {
		t := &s.Tracks[i]
		label := fmt.Sprintf("tracks[%d] %q", i, t.ID)
		if t.Delay != nil && t.Delay.Wet == 0 && t.Delay.Send > 0 {
			out = append(out, fmt.Sprintf("%s: delay.wet is 0, so the delay will be inaudible (delay.send is deprecated and has no effect)", label))
		}
	}
	return out
}

// isKnownArticulation reports whether type is in KnownArticulations.
func isKnownArticulation(t string) bool {
	for _, known := range KnownArticulations {
		if t == known {
			return true
		}
	}
	return false
}
