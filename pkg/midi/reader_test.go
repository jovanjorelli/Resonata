package midi

import (
	"math/rand"
	"os"
	"testing"
)

const assetPath = "../../test_data/mahler_symphony.mid"

func TestReadVLQ(t *testing.T) {
	vectors := []struct {
		bytes []byte
		want  uint32
		size  int
	}{
		{[]byte{0x00}, 0, 1},
		{[]byte{0x40}, 0x40, 1},
		{[]byte{0x7F}, 0x7F, 1},
		{[]byte{0x81, 0x00}, 0x80, 2},
		{[]byte{0xC0, 0x00}, 0x2000, 2},
		{[]byte{0xFF, 0x7F}, 0x3FFF, 2},
		{[]byte{0x81, 0x80, 0x00}, 16384, 3},
		{[]byte{0xFF, 0xFF, 0xFF, 0x7F}, 0x0FFFFFFF, 4},
	}
	for _, tc := range vectors {
		got, pos, err := readVLQ(tc.bytes, 0)
		if err != nil || got != tc.want || pos != tc.size {
			t.Errorf("readVLQ(% x) = %d, %d, %v; want %d, %d", tc.bytes, got, pos, err, tc.want, tc.size)
		}
		// The lenient exported form must agree.
		if v, p := ReadVLQ(tc.bytes, 0); v != tc.want || p != tc.size {
			t.Errorf("ReadVLQ(% x) = %d, %d", tc.bytes, v, p)
		}
	}
	// Errors, never panics.
	if _, _, err := readVLQ([]byte{0x81}, 0); err == nil {
		t.Error("truncated VLQ accepted")
	}
	if _, _, err := readVLQ([]byte{0x81, 0x81, 0x81, 0x81, 0x00}, 0); err == nil {
		t.Error("overlong VLQ accepted")
	}
	if _, _, err := readVLQ(nil, 0); err == nil {
		t.Error("empty data accepted")
	}
	// The lenient form clamps instead of failing.
	if v, p := ReadVLQ([]byte{0x81}, 0); p != 1 {
		t.Errorf("ReadVLQ truncated: v=%d pos=%d", v, p)
	}
}

func TestParseRunningStatus(t *testing.T) {
	// on(60,100) @0, running on(62,100) @96, off(60) @192, running off(62).
	data := []byte{
		0x00, 0x90, 0x3C, 0x64,
		0x60, 0x3E, 0x64,
		0x60, 0x80, 0x3C, 0x40,
		0x00, 0x3E, 0x40,
		0x00, 0xFF, 0x2F, 0x00,
	}
	events, err := parseTrack(data)
	if err != nil {
		t.Fatal(err)
	}
	want := []Event{
		{Tick: 0, Type: EventNoteOn, Data1: 60, Data2: 100},
		{Tick: 96, Type: EventNoteOn, Data1: 62, Data2: 100},
		{Tick: 192, Type: EventNoteOff, Data1: 60, Data2: 64},
		{Tick: 192, Type: EventNoteOff, Data1: 62, Data2: 64},
		{Tick: 192, Type: EventMeta, Data1: MetaEndOfTrack},
	}
	if len(events) != len(want) {
		t.Fatalf("events = %d, want %d", len(events), len(want))
	}
	for i, w := range want {
		g := events[i]
		if g.Tick != w.Tick || g.Type != w.Type || g.Data1 != w.Data1 || g.Data2 != w.Data2 {
			t.Errorf("event %d = %+v, want %+v", i, g, w)
		}
	}
}

func TestRunningStatusCancelledByMetaAndSysex(t *testing.T) {
	// After a meta event, a bare data byte must error (running status was
	// cancelled), not panic or silently reuse the old status.
	bad := []byte{
		0x00, 0x90, 0x3C, 0x64,
		0x00, 0xFF, 0x06, 0x03, 'a', 'b', 'c',
		0x00, 0x3E, 0x64, // running status here is illegal
	}
	if _, err := parseTrack(bad); err == nil {
		t.Error("running status survived a meta event")
	}
	badSysex := []byte{
		0x00, 0x90, 0x3C, 0x64,
		0x00, 0xF0, 0x02, 0x7E, 0xF7,
		0x00, 0x3E, 0x64,
	}
	if _, err := parseTrack(badSysex); err == nil {
		t.Error("running status survived a sysex")
	}
	// Sysex itself parses and cancels cleanly.
	ok := []byte{
		0x00, 0xF0, 0x05, 0x7E, 0x7F, 0x09, 0x01, 0xF7,
		0x00, 0x90, 0x3C, 0x64,
	}
	events, err := parseTrack(ok)
	if err != nil {
		t.Fatal(err)
	}
	if len(events) != 2 || events[0].Type != EventSysex || events[0].Data1 != 0xF0 ||
		len(events[0].Data) != 5 || events[0].Data[4] != 0xF7 {
		t.Fatalf("sysex parsed wrong: %+v", events[0])
	}
}

func TestReadFileAsset(t *testing.T) {
	data, err := os.ReadFile(assetPath)
	if err != nil {
		t.Fatal(err)
	}
	f, err := ReadFile(data)
	if err != nil {
		t.Fatal(err)
	}
	if f.Header.Format != 1 || f.Header.Tracks != 6 || f.Header.TicksPerQuarter != 480 {
		t.Fatalf("header = %+v, want format 1, 6 tracks, 480 PPQ", f.Header)
	}

	// The strings track relies on running status: every note must have
	// survived with correct pitches and bar-aligned ticks.
	strings := f.Tracks[1]
	ons, offs := 0, 0
	sysex := 0
	for _, e := range strings {
		switch {
		case e.Type == EventNoteOn && e.Data2 > 0:
			ons++
			if e.Tick%480 != 0 && e.Tick%1920 != 0 {
				t.Errorf("running-status note at odd tick %d", e.Tick)
			}
		case e.Type == EventNoteOff:
			offs++
		case e.Type == EventSysex:
			sysex++
		}
	}
	if ons != 48 || offs != 48 {
		t.Errorf("strings track: %d ons / %d offs, want 48/48", ons, offs)
	}
	if sysex != 1 {
		t.Errorf("strings sysex count = %d, want 1 (GM on)", sysex)
	}

	// Tempo map: two FF 51 events, both 500000 µs (120 BPM).
	tempos := f.Tempos()
	if len(tempos) != 2 {
		t.Fatalf("tempos = %d, want 2 (restated)", len(tempos))
	}
	for _, tp := range tempos {
		if tp.MicrosPerQuarter != 500000 {
			t.Errorf("tempo = %d µs, want 500000", tp.MicrosPerQuarter)
		}
	}
	if BPMFromMicros(tempos[0].MicrosPerQuarter) != 120 {
		t.Error("BPM conversion wrong")
	}
	tss := f.TimeSignatures()
	if len(tss) != 1 || tss[0].Numerator != 4 || tss[0].Denominator != 4 {
		t.Errorf("time signature = %+v, want one 4/4", tss)
	}
	if TrackName(f.Tracks[0]) != "Mahler Symphony Sketch" {
		t.Errorf("title = %q", TrackName(f.Tracks[0]))
	}
	if TrackName(f.Tracks[1]) != "Strings" {
		t.Errorf("track 1 name = %q", TrackName(f.Tracks[1]))
	}

	// Pitch bend round values: center 0 and +2240 (0x20 LSB, 0x50 MSB →
	// 0x2820 − 8192 = 2336? compute: MSB 0x50<<7 | 0x20 = 10272 − 8192 = 2080).
	winds := f.Tracks[2]
	var bends []int
	for _, e := range winds {
		if e.Type == EventPitchBend {
			bends = append(bends, e.Value)
		}
	}
	if len(bends) != 2 || bends[0] != 2080 || bends[1] != 0 {
		t.Errorf("pitch bends = %v, want [2080 0]", bends)
	}

	// Total note population across the file.
	total := 0
	for _, tr := range f.Tracks {
		total += len(CollectNotes(tr, MaxTick(tr)))
	}
	if total < 250 {
		t.Errorf("collected %d notes, want 250+ from the asset", total)
	}
}

func TestReadFileErrors(t *testing.T) {
	cases := map[string][]byte{
		"empty":          {},
		"junk":           []byte("RIFFxxxxWAVEfmt "),
		"short MThd":     []byte("MThd\x00\x00\x00\x06\x00"),
		"truncated body": append([]byte("MThd\x00\x00\x00\x06"), 0, 1, 0),
		"smpte":          append(append([]byte("MThd\x00\x00\x00\x06"), 0, 1, 0, 1), 0x80|0x60, 0x50),
		"zero ppq":       append(append([]byte("MThd\x00\x00\x00\x06"), 0, 1, 0, 1), 0, 0),
		"no tracks":      append([]byte("MThd\x00\x00\x00\x06"), 0, 1, 0, 1, 0x01, 0xE0),
		"truncated mtrk": append(append(append([]byte("MThd\x00\x00\x00\x06"), 0, 1, 0, 1, 0x01, 0xE0),
			'M', 'T', 'r', 'k'), 0, 0, 0, 99, 0x00, 0x90),
	}
	for name, data := range cases {
		if _, err := ReadFile(data); err == nil {
			t.Errorf("%s: want error, got nil", name)
		}
	}
}

// TestReadFileNeverPanics mutates a valid file pseudorandomly; the parser
// must return errors or results, never panic.
func TestReadFileNeverPanics(t *testing.T) {
	base, err := os.ReadFile(assetPath)
	if err != nil {
		t.Fatal(err)
	}
	rng := rand.New(rand.NewSource(42))
	buf := make([]byte, len(base))
	for i := 0; i < 2000; i++ {
		copy(buf, base)
		mutations := 1 + rng.Intn(6)
		for m := 0; m < mutations; m++ {
			if len(buf) == 0 {
				break
			}
			switch rng.Intn(3) {
			case 0:
				buf[rng.Intn(len(buf))] = byte(rng.Intn(256))
			case 1:
				buf = buf[:rng.Intn(len(buf))+1]
			case 2:
				buf = append(buf, byte(rng.Intn(256)))
			}
		}
		f, err := ReadFile(buf)
		if err == nil && f != nil {
			// Parsed results must also survive note collection.
			for _, tr := range f.Tracks {
				CollectNotes(tr, MaxTick(tr))
			}
		}
		if len(buf) < len(base) {
			// restore length for the next round
			buf = make([]byte, len(base))
		}
	}
}

func TestCollectNotesEdges(t *testing.T) {
	// vel-0 on = off; stray off ignored; LIFO on overlaps; open notes
	// close at endTick.
	events := []Event{
		{Tick: 0, Type: EventNoteOn, Channel: 0, Data1: 60, Data2: 100},
		{Tick: 10, Type: EventNoteOn, Channel: 0, Data1: 60, Data2: 0},  // off
		{Tick: 20, Type: EventNoteOff, Channel: 0, Data1: 72, Data2: 0}, // stray
		{Tick: 30, Type: EventNoteOn, Channel: 0, Data1: 64, Data2: 90},
		{Tick: 40, Type: EventNoteOn, Channel: 0, Data1: 64, Data2: 80}, // retrigger
		{Tick: 50, Type: EventNoteOff, Channel: 0, Data1: 64, Data2: 0}, // closes 40 (LIFO)
		{Tick: 60, Type: EventNoteOff, Channel: 0, Data1: 64, Data2: 0}, // closes 30
		{Tick: 70, Type: EventNoteOn, Channel: 1, Data1: 67, Data2: 70}, // never closed
	}
	notes := CollectNotes(events, 100)
	if len(notes) != 4 {
		t.Fatalf("notes = %d, want 4: %+v", len(notes), notes)
	}
	if notes[0].StartTick != 0 || notes[0].EndTick != 10 || notes[0].Velocity != 100 {
		t.Errorf("vel-0 off: %+v", notes[0])
	}
	// LIFO: the 40-on closes at 50, the 30-on at 60.
	if notes[1].StartTick != 30 || notes[1].EndTick != 60 {
		t.Errorf("outer note: %+v", notes[1])
	}
	if notes[2].StartTick != 40 || notes[2].EndTick != 50 || notes[2].Velocity != 80 {
		t.Errorf("inner note: %+v", notes[2])
	}
	if notes[3].StartTick != 70 || notes[3].EndTick != 100 {
		t.Errorf("open note: %+v", notes[3])
	}
}
