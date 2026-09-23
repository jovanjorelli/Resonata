// Package score defines Resonata's score DSL: the data model, JSON/YAML
// parsers, and schema validation for orchestral scores.
package score

import (
	"encoding/json"
	"fmt"
	"math"
	"strconv"
	"strings"
)

// Score is a complete piece: global metadata plus the tracks to render.
type Score struct {
	Metadata Metadata `json:"metadata"`
	Tracks   []Track  `json:"tracks"`
}

// Metadata describes global score properties.
type Metadata struct {
	Title         string  `json:"title"`
	BPM           float64 `json:"bpm"`
	TimeSignature string  `json:"time_signature"`      // "4/4", "3/4", etc.
	Transpose     int     `json:"transpose,omitempty"` // global semitone shift applied to every track
	Swing         float64 `json:"swing,omitempty"`     // global rhythmic swing 0.0 (straight) to 1.0 (full triplet feel)
	BreathMs      float64 `json:"breath,omitempty"`    // pause between phrases in ms for all tracks unless a track overrides it
}

// Track is one instrument's part: a list of timed notes plus mix settings.
type Track struct {
	ID         string        `json:"id"`
	Name       string        `json:"name"`
	Instrument InstrumentDef `json:"instrument"`
	Pan        float32       `json:"pan"`                   // -1.0 (left) to 1.0 (right)
	Volume     float32       `json:"volume"`                // 0.0 to 1.0
	Transpose  int           `json:"transpose,omitempty"`   // per-track semitone shift, added to the global transpose
	Swing      float64       `json:"swing,omitempty"`       // per-track swing 0.0 to 1.0, overrides the global swing when non-zero
	Vibrato    *Vibrato      `json:"vibrato,omitempty"`     // track pitch LFO for all notes unless a note overrides it
	Expression *float64      `json:"expression,omitempty"`  // track dynamic gain 0.0-1.0 unless a note overrides it
	BreathMs   float64       `json:"breath,omitempty"`      // pause between phrases in ms, overrides the global breath when non-zero
	Room       *Room         `json:"room,omitempty"`        // track acoustic space, overrides the master reverb preset
	ReverbSend float32       `json:"reverb_send,omitempty"` // 0.0 to 1.0 master reverb send
	EQ         TrackEQ       `json:"eq,omitempty"`          // per-track parametric sculpt
	Delay      *TrackDelay   `json:"delay,omitempty"`       // per-track echo send and space
	Notes      []NoteEvent   `json:"notes"`
}

// TrackDelay configures the stereo echo for one track. Every track
// carrying a delay gets a fully independent echo instance: its own tap
// length, feedback, damping, wet level, and mode. There is no shared
// delay bus.
type TrackDelay struct {
	Mode        string  `json:"mode,omitempty"`        // "stereo" | "pingpong"
	Subdivision string  `json:"subdivision,omitempty"` // "1/4", "1/8", "1/8d", "1/16", "1/16t"
	Seconds     float64 `json:"seconds,omitempty"`     // manual delay time override in seconds
	Feedback    float32 `json:"feedback,omitempty"`    // 0.0 to 0.98 loop gain
	DampingHz   float32 `json:"damping_hz,omitempty"`  // 1-pole lowpass cutoff, default 4000
	Wet         float32 `json:"wet,omitempty"`         // 0.0 to 1.0 track echo return mix
	Send        float32 `json:"send,omitempty"`        // deprecated: parsed but ignored
}

// EQBand is one parametric EQ band in the score DSL.
type EQBand struct {
	Freq float32 `json:"freq"` // Hz; 0 disables the band
	Gain float32 `json:"gain"` // dB
	Q    float32 `json:"q"`    // resonance; 0 selects the band-type default
}

// TrackEQ sculpts one track: an optional highpass (mud/rumble removal)
// plus low shelf, mid peak, and high shelf bands. Zero frequencies
// disable a stage.
type TrackEQ struct {
	HPF       float32 `json:"hpf,omitempty"`     // Hz (e.g., 80.0)
	LowShelf  EQBand  `json:"eq_low,omitempty"`  // {freq, gain, q}
	MidPeak   EQBand  `json:"eq_mid,omitempty"`  // {freq, gain, q}
	HighShelf EQBand  `json:"eq_high,omitempty"` // {freq, gain, q}
}

// InstrumentDef selects the sound source for a track. Synthesized
// instruments need only Type; sampler instruments reference an SFZ file.
type InstrumentDef struct {
	Type       string             `json:"type"`                 // "sampler", "ocarina"
	File       string             `json:"file,omitempty"`       // SFZ file path
	Parameters map[string]float32 `json:"parameters,omitempty"` // instrument-specific knobs
}

// NoteEvent is a single note with timing in seconds.
type NoteEvent struct {
	Time           float64      `json:"time"`                       // seconds
	Duration       float64      `json:"duration"`                   // seconds
	Pitch          int          `json:"pitch"`                      // MIDI note (60 = C4)
	Velocity       float32      `json:"velocity"`                   // 0.0 to 1.0
	TimingOffsetMs float64      `json:"timing_offset_ms,omitempty"` // deliberate micro-timing shift, applied at parse
	Accent         float64      `json:"accent,omitempty"`           // velocity multiplier, 1.0 = no change
	AttackSec      float64      `json:"attack,omitempty"`           // per-note attack override in seconds, 0 = SFZ value
	DecaySec       float64      `json:"decay,omitempty"`            // per-note decay override in seconds, 0 = SFZ value
	ReleaseSec     float64      `json:"release,omitempty"`          // per-note release override in seconds, 0 = SFZ value
	PhraseID       int          `json:"phrase_id,omitempty"`        // phrase group tag, 0 = no named phrase
	Vibrato        *Vibrato     `json:"vibrato,omitempty"`          // per-note pitch LFO, overrides the track vibrato
	Expression     *float64     `json:"expression,omitempty"`       // per-note dynamic gain 0.0-1.0, overrides the track expression
	Articulation   Articulation `json:"articulation,omitempty"`
}

// Articulation attaches a playing-style modifier to a note.
type Articulation struct {
	Type   string                 `json:"type"` // "staccato", "legato", "tenuto"
	Params map[string]interface{} `json:"params,omitempty"`
}

// End returns the note's end time in seconds.
func (n NoteEvent) End() float64 { return n.Time + n.Duration }

// PitchName returns the scientific name of a MIDI note, e.g. 60 -> "C4".
func (n NoteEvent) PitchName() string {
	names := [12]string{"C", "C#", "D", "D#", "E", "F", "F#", "G", "G#", "A", "A#", "B"}
	return fmt.Sprintf("%s%d", names[((n.Pitch%12)+12)%12], n.Pitch/12-1)
}

// ApplyTransposition shifts every note by its effective transposition:
// the global metadata transpose plus the track transpose, in semitones.
// It runs at parse time so downstream consumers (engine scheduling,
// sampler region matching, ocarina pitch) receive final pitches without
// knowing about transposition. Zero shifts leave notes untouched. The
// original pitches are not preserved; Validate reports out-of-range
// results with full context.
func (s *Score) ApplyTransposition() {
	for i := range s.Tracks {
		shift := s.Metadata.Transpose + s.Tracks[i].Transpose
		if shift == 0 {
			continue
		}
		for j := range s.Tracks[i].Notes {
			s.Tracks[i].Notes[j].Pitch += shift
		}
	}
}

// ApplyTimingOffsets shifts every note with a non-zero timing_offset_ms
// by that many milliseconds (positive delays, negative advances). Times
// clamp silently into [0, score end] so an offset never escapes the
// piece. Runs at parse time after transposition and before swing, so
// deliberate offsets land before any humanize jitter.
func (s *Score) ApplyTimingOffsets() {
	end := 0.0
	for i := range s.Tracks {
		for _, n := range s.Tracks[i].Notes {
			if e := n.End(); e > end {
				end = e
			}
		}
	}
	for i := range s.Tracks {
		for j := range s.Tracks[i].Notes {
			off := s.Tracks[i].Notes[j].TimingOffsetMs
			if off == 0 {
				continue
			}
			t := s.Tracks[i].Notes[j].Time + off/1000
			if !(t >= 0) {
				t = 0
			} else if t > end {
				t = end
			}
			s.Tracks[i].Notes[j].Time = t
		}
	}
}

// swingTolerance is the beat-fraction window around 0.5 inside which a
// note counts as an eighth-note off-beat ("and" of the beat).
const swingTolerance = 0.1

// beatDuration returns the beat length in seconds from the tempo and
// time signature: a 4/4 beat is 60/BPM, scaled by 4 over the signature
// denominator. It returns 0 when the tempo or signature is unusable, in
// which case swing is skipped and validation reports the bad field.
func beatDuration(bpm float64, timeSignature string) float64 {
	if !(bpm > 0) || math.IsInf(bpm, 0) || math.IsNaN(bpm) {
		return 0
	}
	den := 4
	if _, d, ok := strings.Cut(timeSignature, "/"); ok {
		if n, err := strconv.Atoi(strings.TrimSpace(d)); err == nil && n > 0 {
			den = n
		} else {
			return 0
		}
	}
	return 60 / bpm * 4 / float64(den)
}

// ApplySwing delays eighth-note off-beats per track: notes sitting near
// the "and" of a beat (fraction 0.5 ± 0.1) shift later by
// swing * beat / 3, so 1.0 gives full triplet feel and 0.5 halves it.
// The track swing overrides the metadata swing when non-zero; both
// default to straight time. Runs at parse time after timing offsets.
func (s *Score) ApplySwing() {
	beat := beatDuration(s.Metadata.BPM, s.Metadata.TimeSignature)
	if !(beat > 0) || math.IsInf(beat, 0) {
		return
	}
	for i := range s.Tracks {
		swing := s.Tracks[i].Swing
		if swing == 0 {
			swing = s.Metadata.Swing
		}
		if swing == 0 {
			continue
		}
		delay := swing * beat / 3
		for j := range s.Tracks[i].Notes {
			frac := math.Mod(s.Tracks[i].Notes[j].Time/beat, 1)
			if math.Abs(frac-0.5) <= swingTolerance {
				s.Tracks[i].Notes[j].Time += delay
			}
		}
	}
}

// ApplyAccents scales every note carrying an accent other than 0 or 1.0:
// final velocity is base velocity times accent, clamped to [0, 1]. An
// accent above 1.0 emphasizes the note, below 1.0 softens it. Runs at
// parse time; the velocity field holds the final value afterwards.
func (s *Score) ApplyAccents() {
	for i := range s.Tracks {
		for j := range s.Tracks[i].Notes {
			a := s.Tracks[i].Notes[j].Accent
			if a == 0 || a == 1 {
				continue
			}
			v := float64(s.Tracks[i].Notes[j].Velocity) * a
			if !(v >= 0) {
				v = 0
			} else if v > 1 {
				v = 1
			}
			s.Tracks[i].Notes[j].Velocity = float32(v)
		}
	}
}

// Vibrato is an optional pitch LFO on a note or track: rate in Hz
// (typically 3.0-8.0, default 5.0) with depth in semitones (typically
// 0.01-0.15, default 0.03). Absent means no vibrato; a note vibrato
// overrides the track vibrato.
type Vibrato struct {
	Rate  float64 `json:"rate,omitempty"`
	Depth float64 `json:"depth,omitempty"`
}

// RoomConfig is an explicit acoustic space: room size (tail length),
// damping (HF absorption), and stereo width, each 0.0 to 1.0.
type RoomConfig struct {
	Size    float64 `json:"size,omitempty"`
	Damping float64 `json:"damping,omitempty"`
	Width   float64 `json:"width,omitempty"`
}

// Room selects a track's acoustic space, overriding the master reverb
// preset for that track's send. It decodes from either a preset-name
// string ("cathedral", "hall", "room", "none") or an object with size,
// damping, and width. Absent (nil) means the track uses the master
// reverb. "none" renders the track dry.
type Room struct {
	IsPreset bool
	Preset   string
	Config   RoomConfig
}

// roomPresets lists the preset names accepted as a room string.
var roomPresets = []string{"cathedral", "hall", "room", "none"}

// IsRoomPreset reports whether name is an accepted room preset.
func IsRoomPreset(name string) bool {
	for _, p := range roomPresets {
		if p == name {
			return true
		}
	}
	return false
}

// UnmarshalJSON decodes a preset-name string or a size/damping/width
// object. JSON null leaves the Room zero-valued.
func (r *Room) UnmarshalJSON(data []byte) error {
	trimmed := strings.TrimSpace(string(data))
	if trimmed == "" || trimmed == "null" {
		return nil
	}
	if strings.HasPrefix(trimmed, "\"") {
		var name string
		if err := json.Unmarshal(data, &name); err != nil {
			return fmt.Errorf("room: %w", err)
		}
		r.IsPreset = true
		r.Preset = strings.ToLower(strings.TrimSpace(name))
		return nil
	}
	var cfg RoomConfig
	if err := json.Unmarshal(data, &cfg); err != nil {
		return fmt.Errorf("room: %w", err)
	}
	r.IsPreset = false
	r.Config = cfg
	return nil
}

// TotalNotes returns the combined note count of all tracks.
func (s *Score) TotalNotes() int {
	total := 0
	for i := range s.Tracks {
		total += len(s.Tracks[i].Notes)
	}
	return total
}

// Duration returns the end time of the latest note in seconds; an empty
// score has duration 0.
func (s *Score) Duration() float64 {
	end := 0.0
	for i := range s.Tracks {
		for _, n := range s.Tracks[i].Notes {
			if e := n.End(); e > end {
				end = e
			}
		}
	}
	return end
}
