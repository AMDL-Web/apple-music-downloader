package progress

import (
	"math"
	"sort"
	"time"

	"amdl/internal/domain"
)

// TickMax is the most tracks the album bar is cut into cells for; above it the
// bar is one continuous run (the iOS app's segmentedTicks).
const TickMax = 24

// Run is one stretch of the album bar's solid fill: consecutive finished tracks
// with the same outcome.
type Run struct {
	// Status is the tracks' final status: completed, skipped_existing, failed
	// or cancelled. A single unfinished track reports its own status.
	Status domain.ItemStatus `json:"status"`
	// Share is the run's part of the bar's width, in [0,1].
	Share float64 `json:"share"`
}

// Album is a job's album bar, after the iOS app's SegmentedProgressBar: one bar
// with a solid fill for the tracks that have finished, in the order they
// finished, and a translucent fill for the overall stage-weighted progress.
// The web console draws the same bar from albumProgress in
// apps/console/static/js/lib/album-progress.js.
type Album struct {
	// Total is the number of tracks and Lit how many have finished.
	Total int `json:"total"`
	Lit   int `json:"lit"`
	// Runs is the solid fill, left to right, in finish order.
	Runs []Run `json:"runs"`
	// Done is where the solid fill ends and Live where the translucent fill
	// ends; Live is never behind Done.
	Done float64 `json:"done"`
	Live float64 `json:"live"`
	// Percent is the overall progress. It stays below 100 while a track is
	// still working and is 100 once the job completes.
	Percent int `json:"percent"`
	// Ticks is the number of cells to cut the bar into, 0 for none (one track,
	// or more than TickMax).
	Ticks int `json:"ticks"`
}

// ItemFraction is one track's progress in [0,1] under Weights, from its flags
// and meters alone. Completed and
// already-on-disk tracks count as 1; a failed or cancelled track stays where it
// stopped.
func ItemFraction(it domain.JobItem) float64 {
	if it.Status == domain.ItemCompleted || it.Status == domain.ItemSkipped {
		return 1
	}
	p := it.Progress
	if p.Saved {
		return 1
	}
	flag := func(on bool, s Stage) float64 {
		if on {
			return Weights[s]
		}
		return 0
	}
	return clamp01(flag(p.Resolved, StageResolve) +
		Weights[StageDownload]*clamp01(p.Download) +
		Weights[StageDecrypt]*clamp01(p.Decrypt) +
		flag(p.Remuxed, StageRemux) +
		flag(p.Verified, StageVerify) +
		flag(p.Tagged, StageTag))
}

// outcomeRank orders finished tracks whose finish times tie or are unknown.
var outcomeRank = map[domain.ItemStatus]int{
	domain.ItemCompleted: 0,
	domain.ItemSkipped:   1,
	domain.ItemFailed:    2,
	domain.ItemCancelled: 3,
}

// AlbumProgress computes the album bar of a job from its status and items.
func AlbumProgress(status domain.JobStatus, items []domain.JobItem) Album {
	total := len(items)
	var finished []domain.JobItem
	for _, it := range items {
		if _, ok := outcomeRank[it.Status]; ok {
			finished = append(finished, it)
		}
	}
	sort.SliceStable(finished, func(i, j int) bool {
		a, b := finished[i], finished[j]
		if ta, tb := finishTime(a), finishTime(b); !ta.Equal(tb) {
			return ta.Before(tb)
		}
		if ra, rb := outcomeRank[a.Status], outcomeRank[b.Status]; ra != rb {
			return ra < rb
		}
		return a.Index < b.Index
	})
	overall := Overall(status, items)

	var runs []Run
	if total == 1 {
		st, share := items[0].Status, overall
		if len(finished) == 1 {
			share = 1
		}
		runs = append(runs, Run{Status: st, Share: share})
	} else {
		for _, it := range finished {
			if n := len(runs); n > 0 && runs[n-1].Status == it.Status {
				runs[n-1].Share += 1 / float64(total)
			} else {
				runs = append(runs, Run{Status: it.Status, Share: 1 / float64(total)})
			}
		}
	}
	out := Album{Total: total, Lit: len(finished), Percent: int(math.Round(overall * 100))}
	for _, r := range runs {
		if r.Share > 0 {
			out.Runs = append(out.Runs, r)
			out.Done += r.Share
		}
	}
	out.Done = min(1, out.Done)
	out.Live = out.Done
	if total > 1 {
		out.Live = max(out.Done, overall)
	}
	if total >= 2 && total <= TickMax {
		out.Ticks = total
	}
	return out
}

// Overall is a job's progress in [0,1]: the mean of its tracks' ItemFraction,
// held below 1 while a track has not finished and 1 once the job completed.
// Album.Percent is Overall in percent.
func Overall(status domain.JobStatus, items []domain.JobItem) float64 {
	mean := 0.0
	working := false
	for _, it := range items {
		if _, ok := outcomeRank[it.Status]; !ok {
			working = true
		}
		mean += ItemFraction(it)
	}
	if len(items) > 0 {
		mean /= float64(len(items))
	}
	switch {
	case status == domain.JobCompleted:
		return 1
	case working:
		return min(mean, 0.99)
	}
	return mean
}

// finishTime is when a finished track reached its final status; an unknown time
// sorts last.
func finishTime(it domain.JobItem) time.Time {
	if it.UpdatedAt.IsZero() {
		return time.Unix(math.MaxInt32, 0)
	}
	return it.UpdatedAt
}

func clamp01(v float64) float64 {
	if v > 0 {
		return min(1, v)
	}
	return 0
}
