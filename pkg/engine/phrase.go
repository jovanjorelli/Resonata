package engine

import (
	"sort"

	"resonata/pkg/dsp"
	"resonata/pkg/score"
)

// maxEngineRooms caps the distinct per-track acoustic spaces collected
// during initialization; the least-used overflow merges into the
// nearest surviving space.
const maxEngineRooms = 8

// breathTimes returns each note's start time with phrase breath pauses
// applied: walking notes in start-time order, every transition between
// two differing non-zero phrase IDs delays the new phrase and all later
// notes by the breath gap (track value, else the global one). Zero
// breath, short tracks, and any transition touching phrase zero leave
// times untouched. The score itself is never mutated.
func breathTimes(tr *score.Track, globalBreathMs float64) []float64 {
	times := make([]float64, len(tr.Notes))
	for j := range tr.Notes {
		times[j] = tr.Notes[j].Time
	}
	breath := tr.BreathMs
	if breath == 0 {
		breath = globalBreathMs
	}
	if breath <= 0 || len(times) < 2 {
		return times
	}
	gap := breath / 1000
	order := make([]int, len(times))
	for j := range order {
		order[j] = j
	}
	sort.SliceStable(order, func(a, b int) bool {
		return times[order[a]] < times[order[b]]
	})
	shift, prev := 0.0, 0
	for k, j := range order {
		id := tr.Notes[j].PhraseID
		if k > 0 && id != 0 && prev != 0 && id != prev {
			shift += gap
		}
		times[j] += shift
		prev = id
	}
	return times
}

// roomEntry is one distinct track acoustic space with its usage count.
type roomEntry struct {
	params dsp.ReverbParams
	uses   int
}

// roomDistance compares spaces over size, damping, and width.
func roomDistance(a, b dsp.ReverbParams) float64 {
	ds := float64(a.RoomSize - b.RoomSize)
	dd := float64(a.Damping - b.Damping)
	dw := float64(a.Width - b.Width)
	return ds*ds + dd*dd + dw*dw
}

// resolveRoom maps a score room onto FDN parameters. It reports none
// for the "none" preset (the track renders dry) and false when the
// value cannot resolve (unknown preset), in which case the caller keeps
// the master reverb.
func resolveRoom(r *score.Room) (dsp.ReverbParams, bool, bool) {
	if r.IsPreset {
		if r.Preset == "none" {
			return dsp.ReverbParams{}, true, true
		}
		p, ok := dsp.PresetParams(r.Preset)
		return p, false, ok
	}
	return dsp.ReverbParams{
		RoomSize: dsp.ClampF32(float32(r.Config.Size), 0, 1),
		Damping:  dsp.ClampF32(float32(r.Config.Damping), 0, 1),
		Width:    dsp.ClampF32(float32(r.Config.Width), 0, 1),
		Wet:      0.4,
		Dry:      0.6,
		PreDelay: 0.02,
	}, false, true
}

// assignRooms binds every track's acoustic space on the mixer: tracks
// without a room keep the master reverb, "none" tracks render dry, and
// tracks sharing one configuration share one FDN instance. More than
// maxEngineRooms distinct spaces merge least-used-first into the
// nearest survivor. All FDN allocation happens here at setup time.
func (e *Engine) assignRooms(s *score.Score) {
	type pending struct {
		track int
		none  bool
		entry int // index into entries, -1 until merged
	}
	var (
		entries []roomEntry
		items   []pending
	)
	for i := range s.Tracks {
		r := s.Tracks[i].Room
		if r == nil {
			continue // master reverb
		}
		params, none, ok := resolveRoom(r)
		if !ok {
			continue // unknown preset: master reverb
		}
		if none {
			items = append(items, pending{track: i, none: true})
			continue
		}
		at := -1
		for k := range entries {
			if entries[k].params == params {
				at = k
				break
			}
		}
		if at < 0 {
			at = len(entries)
			entries = append(entries, roomEntry{params: params})
		}
		entries[at].uses++
		items = append(items, pending{track: i, entry: at})
	}
	for len(entries) > maxEngineRooms {
		// Merge the least-used space into its nearest survivor and
		// repoint every affected track.
		victim := 0
		for k := range entries {
			if entries[k].uses < entries[victim].uses {
				victim = k
			}
		}
		best, bestD := -1, -1.0
		for k := range entries {
			if k == victim {
				continue
			}
			if d := roomDistance(entries[victim].params, entries[k].params); best < 0 || d < bestD {
				best, bestD = k, d
			}
		}
		for k := range items {
			if items[k].entry == victim {
				items[k].entry = best
			} else if items[k].entry > victim {
				items[k].entry--
			}
		}
		entries = append(entries[:victim], entries[victim+1:]...)
	}
	for _, it := range items {
		if it.none {
			e.mix.SetTrackRoomNone(it.track)
			continue
		}
		e.mix.SetTrackRoom(it.track, entries[it.entry].params)
	}
}
