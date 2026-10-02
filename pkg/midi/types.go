// Package midi implements Standard MIDI File (SMF) reading, writing, and
// bidirectional conversion with Resonata scores. It is pure Go with zero
// external dependencies and tolerant of real-world files: running status,
// system exclusives, meta events, and unknown chunks parse cleanly, and
// malformed input yields errors instead of panics.
package midi

import "sort"

// SMF chunk identifiers.
const (
	chunkHeader = "MThd"
	chunkTrack  = "MTrk"
)

// EventType classifies a parsed track event.
type EventType uint8

// Event types: the seven channel messages plus sysex and meta.
const (
	EventNoteOff         EventType = iota // 0x8n
	EventNoteOn                           // 0x9n
	EventPolyPressure                     // 0xAn
	EventControlChange                    // 0xBn
	EventProgramChange                    // 0xCn
	EventChannelPressure                  // 0xDn
	EventPitchBend                        // 0xEn
	EventSysex                            // 0xF0/0xF7 (and rare system-common)
	EventMeta                             // 0xFF
)

// Meta event types of interest.
const (
	MetaText          = 0x01
	MetaCopyright     = 0x02
	MetaTrackName     = 0x03
	MetaInstrument    = 0x04
	MetaMarker        = 0x06
	MetaEndOfTrack    = 0x2F
	MetaTempo         = 0x51
	MetaTimeSignature = 0x58
	MetaKeySignature  = 0x59
)

// Event is one parsed SMF event at an absolute tick position.
type Event struct {
	Tick    uint32 // absolute time in PPQ ticks
	Type    EventType
	Channel int    // 0-15 for channel messages
	Data1   int    // note/CC number, meta type, or sysex status
	Data2   int    // velocity or CC value
	Value   int    // pitch bend, signed around center (-8192..8191)
	Data    []byte // meta/sysex payload (owned copy)
}

// Header is a parsed MThd chunk.
type Header struct {
	Format          int // 0 (single track) or 1 (multi-track)
	Tracks          int // MTrk chunks actually parsed
	TicksPerQuarter int // PPQ timing resolution
}

// File is a parsed Standard MIDI File.
type File struct {
	Header Header
	Tracks [][]Event
}

// Note is a paired note-on/off span in absolute ticks.
type Note struct {
	Channel   int
	Pitch     int
	Velocity  int // note-on velocity, 1-127
	StartTick uint32
	EndTick   uint32
}

// Tempo is one FF 51 tempo change.
type Tempo struct {
	Tick             uint32
	MicrosPerQuarter int
}

// TimeSig is one FF 58 time signature.
type TimeSig struct {
	Tick        uint32
	Numerator   int
	Denominator int
}

// BPMFromMicros converts microseconds-per-quarter to BPM.
func BPMFromMicros(mpq int) float64 { return 60e6 / float64(mpq) }

// MicrosFromBPM converts BPM to microseconds-per-quarter, clamped to the
// 3-byte meta range (1..1,000,000 BPM).
func MicrosFromBPM(bpm float64) int {
	if !(bpm > 0) {
		bpm = 120
	}
	mpq := int(roundF(60e6 / bpm))
	if mpq < 60 {
		mpq = 60
	}
	if mpq > 6000000 {
		mpq = 6000000
	}
	return mpq
}

// Tempos returns every FF 51 tempo event across all tracks, sorted by
// tick with duplicates at the same tick collapsed (first wins).
func (f *File) Tempos() []Tempo {
	var out []Tempo
	for _, tr := range f.Tracks {
		for _, e := range tr {
			if e.Type == EventMeta && e.Data1 == MetaTempo && len(e.Data) >= 3 {
				mpq := int(e.Data[0])<<16 | int(e.Data[1])<<8 | int(e.Data[2])
				if mpq > 0 {
					out = append(out, Tempo{Tick: e.Tick, MicrosPerQuarter: mpq})
				}
			}
		}
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].Tick < out[j].Tick })
	dedup := out[:0]
	for i, t := range out {
		if i == 0 || t.Tick != out[i-1].Tick {
			dedup = append(dedup, t)
		}
	}
	return dedup
}

// TimeSignatures returns every FF 58 event across all tracks, sorted by
// tick.
func (f *File) TimeSignatures() []TimeSig {
	var out []TimeSig
	for _, tr := range f.Tracks {
		for _, e := range tr {
			if e.Type == EventMeta && e.Data1 == MetaTimeSignature && len(e.Data) >= 2 {
				denomPow := int(e.Data[1])
				if denomPow < 0 || denomPow > 5 {
					continue
				}
				out = append(out, TimeSig{Tick: e.Tick, Numerator: int(e.Data[0]), Denominator: 1 << denomPow})
			}
		}
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].Tick < out[j].Tick })
	return out
}

// TrackName returns the first track-name (FF 03) or instrument-name
// (FF 04) meta text, or "".
func TrackName(events []Event) string {
	for _, kind := range []int{MetaTrackName, MetaInstrument} {
		for _, e := range events {
			if e.Type == EventMeta && e.Data1 == kind && len(e.Data) > 0 {
				return string(e.Data)
			}
		}
	}
	return ""
}

// MaxTick returns the largest absolute tick in an event slice.
func MaxTick(events []Event) uint32 {
	var max uint32
	for _, e := range events {
		if e.Tick > max {
			max = e.Tick
		}
	}
	return max
}

// CollectNotes pairs note-ons with note-offs. A note-on with velocity 0
// acts as an off; overlapping re-triggers close the newest open note
// (LIFO); stray offs are ignored; notes still open close at endTick.
// The result is sorted by (start tick, pitch).
func CollectNotes(events []Event, endTick uint32) []Note {
	type key struct{ ch, pitch int }
	type pending struct {
		tick uint32
		vel  int
	}
	open := make(map[key][]pending)
	var notes []Note

	closeNote := func(k key, p pending, end uint32) {
		if end <= p.tick {
			end = p.tick + 1
		}
		notes = append(notes, Note{
			Channel: k.ch, Pitch: k.pitch, Velocity: p.vel,
			StartTick: p.tick, EndTick: end,
		})
	}

	for _, e := range events {
		switch e.Type {
		case EventNoteOn:
			k := key{e.Channel, e.Data1}
			if e.Data2 > 0 {
				open[k] = append(open[k], pending{e.Tick, e.Data2})
				continue
			}
			fallthrough // velocity 0 is a note-off
		case EventNoteOff:
			k := key{e.Channel, e.Data1}
			st := open[k]
			if len(st) == 0 {
				continue // stray note-off
			}
			closeNote(k, st[len(st)-1], e.Tick)
			open[k] = st[:len(st)-1]
		}
	}
	for k, stack := range open {
		for _, p := range stack {
			closeNote(k, p, endTick)
		}
	}
	sort.SliceStable(notes, func(i, j int) bool {
		if notes[i].StartTick != notes[j].StartTick {
			return notes[i].StartTick < notes[j].StartTick
		}
		return notes[i].Pitch < notes[j].Pitch
	})
	return notes
}

// GMProgramNames lists the 128 General MIDI patch names (index = program).
var GMProgramNames = [128]string{
	"Acoustic Grand Piano", "Bright Acoustic Piano", "Electric Grand Piano", "Honky-tonk Piano",
	"Electric Piano 1", "Electric Piano 2", "Harpsichord", "Clavinet",
	"Celesta", "Glockenspiel", "Music Box", "Vibraphone",
	"Marimba", "Xylophone", "Tubular Bells", "Dulcimer",
	"Drawbar Organ", "Percussive Organ", "Rock Organ", "Church Organ",
	"Reed Organ", "Accordion", "Harmonica", "Tango Accordion",
	"Acoustic Guitar (nylon)", "Acoustic Guitar (steel)", "Electric Guitar (jazz)", "Electric Guitar (clean)",
	"Electric Guitar (muted)", "Overdriven Guitar", "Distortion Guitar", "Guitar Harmonics",
	"Acoustic Bass", "Electric Bass (finger)", "Electric Bass (pick)", "Fretless Bass",
	"Slap Bass 1", "Slap Bass 2", "Synth Bass 1", "Synth Bass 2",
	"Violin", "Viola", "Cello", "Contrabass",
	"Tremolo Strings", "Pizzicato Strings", "Orchestral Harp", "Timpani",
	"String Ensemble 1", "String Ensemble 2", "Synth Strings 1", "Synth Strings 2",
	"Choir Aahs", "Voice Oohs", "Synth Voice", "Orchestra Hit",
	"Trumpet", "Trombone", "Tuba", "Muted Trumpet",
	"French Horn", "Brass Section", "Synth Brass 1", "Synth Brass 2",
	"Soprano Sax", "Alto Sax", "Tenor Sax", "Baritone Sax",
	"Oboe", "English Horn", "Bassoon", "Clarinet",
	"Piccolo", "Flute", "Recorder", "Pan Flute",
	"Blown Bottle", "Shakuhachi", "Whistle", "Ocarina",
	"Lead 1 (square)", "Lead 2 (sawtooth)", "Lead 3 (calliope)", "Lead 4 (chiff)",
	"Lead 5 (charang)", "Lead 6 (voice)", "Lead 7 (fifths)", "Lead 8 (bass + lead)",
	"Pad 1 (new age)", "Pad 2 (warm)", "Pad 3 (polysynth)", "Pad 4 (choir)",
	"Pad 5 (bowed)", "Pad 6 (metallic)", "Pad 7 (halo)", "Pad 8 (sweep)",
	"FX 1 (rain)", "FX 2 (soundtrack)", "FX 3 (crystal)", "FX 4 (atmosphere)",
	"FX 5 (brightness)", "FX 6 (goblins)", "FX 7 (echoes)", "FX 8 (sci-fi)",
	"Sitar", "Banjo", "Shamisen", "Koto",
	"Kalimba", "Bagpipe", "Fiddle", "Shanai",
	"Tinkle Bell", "Agogo", "Steel Drums", "Woodblock",
	"Taiko Drum", "Melodic Tom", "Synth Drum", "Reverse Cymbal",
	"Guitar Fret Noise", "Breath Noise", "Seashore", "Bird Tweet",
	"Telephone Ring", "Helicopter", "Applause", "Gunshot",
}

// roundF rounds to the nearest integer, half away from zero.
func roundF(v float64) float64 {
	if v < 0 {
		return float64(int64(v - 0.5))
	}
	return float64(int64(v + 0.5))
}
