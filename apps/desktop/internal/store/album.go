package store

import (
	"sort"
	"time"

	"amdl/internal/domain"
)

// The album bar mirrors the web console's lib/album-progress.js
// (albumProgress), itself after the iOS app's SegmentedProgressBar: keep the
// two in step, so both clients show one number.

// AlbumTicksMax is the most tracks the album bar is cut into cells for
// (iOS segmentedTicks); above it the bar is drawn whole.
const AlbumTicksMax = 24

// outcomeRank orders finished tracks whose finish times tie or are unknown.
var outcomeRank = map[domain.ItemStatus]int{
	domain.ItemCompleted: 0, domain.ItemSkipped: 1, domain.ItemFailed: 2, domain.ItemCancelled: 3,
}

// AlbumRun is a stretch of the album bar's solid fill: finished tracks of
// one outcome, Share of the bar's width. Status is "" for a single track
// still working.
type AlbumRun struct {
	Status domain.ItemStatus
	Share  float64
}

// Album is the album bar of a job: a solid fill for the tracks that
// finished, lit left to right in the order they finished and colored by
// outcome, and a translucent fill to the overall progress behind it.
type Album struct {
	Total, Lit int
	Runs       []AlbumRun
	// Done is where the solid fill ends and Live where the translucent one
	// does, both 0..1; Live is never behind Done.
	Done, Live float64
	// Percent is the overall progress: the mean of ItemFraction, held below
	// 100 while a track still works, 100 once the job completes.
	Percent int
	// Ticks is the cells to cut the bar into, 0 for none.
	Ticks int
}

// ItemFraction is one track's progress, 0..1, weighted by StageWeights.
// Completed and already-on-disk tracks count whole; a failed or cancelled
// one stays where it stopped.
func ItemFraction(it domain.JobItem) float64 {
	if it.Status == domain.ItemCompleted || it.Status == domain.ItemSkipped || it.Progress.Saved {
		return 1
	}
	p, w := it.Progress, StageWeights
	f := w[StageDownload]*clamp01(p.Download) + w[StageDecrypt]*clamp01(p.Decrypt)
	for _, s := range []struct {
		on    bool
		stage Stage
	}{{p.Resolved, StageResolve}, {p.Remuxed, StageRemux}, {p.Verified, StageVerify}, {p.Tagged, StageTag}} {
		if s.on {
			f += w[s.stage]
		}
	}
	return clamp01(f / 100)
}

func clamp01(v float64) float64 { return min(max(v, 0), 1) }

// Album is the job's album bar, from the tracks the app has followed.
func (j *Job) Album() Album { return AlbumProgress(j.Status, j.Items) }

// AlbumProgress is the album bar of a job in status from its tracks.
func AlbumProgress(status domain.JobStatus, items []domain.JobItem) Album {
	total := len(items)
	var finished []domain.JobItem
	mean := 0.0
	for _, it := range items {
		if _, ok := outcomeRank[it.Status]; ok {
			finished = append(finished, it)
		}
		mean += ItemFraction(it)
	}
	if total > 0 {
		mean /= float64(total)
	}
	sort.SliceStable(finished, func(i, j int) bool {
		a, b := finished[i], finished[j]
		switch {
		case !a.UpdatedAt.Equal(b.UpdatedAt):
			return finishTime(a).Before(finishTime(b))
		case outcomeRank[a.Status] != outcomeRank[b.Status]:
			return outcomeRank[a.Status] < outcomeRank[b.Status]
		}
		return a.Index < b.Index
	})
	working := len(finished) < total
	overall := mean
	switch {
	case status == domain.JobCompleted:
		overall = 1
	case working:
		overall = min(mean, 0.99)
	}
	var runs []AlbumRun
	if total == 1 {
		r := AlbumRun{Share: overall}
		if len(finished) == 1 {
			r = AlbumRun{Status: finished[0].Status, Share: 1}
		}
		runs = append(runs, r)
	} else {
		for _, it := range finished {
			if n := len(runs); n > 0 && runs[n-1].Status == it.Status {
				runs[n-1].Share += 1 / float64(total)
			} else {
				runs = append(runs, AlbumRun{Status: it.Status, Share: 1 / float64(total)})
			}
		}
	}
	a := Album{Total: total, Lit: len(finished), Percent: int(overall*100 + 0.5)}
	for _, r := range runs {
		if r.Share > 0 {
			a.Runs = append(a.Runs, r)
			a.Done += r.Share
		}
	}
	a.Done = min(a.Done, 1)
	a.Live = a.Done
	if total > 1 {
		a.Live = max(a.Done, overall)
	}
	if total >= 2 && total <= AlbumTicksMax {
		a.Ticks = total
	}
	return a
}

// finishTime treats a track without a time as finishing last, as the
// console does with an unparsable one.
func finishTime(it domain.JobItem) time.Time {
	if it.UpdatedAt.IsZero() {
		return farFuture
	}
	return it.UpdatedAt
}

var farFuture = time.Date(9999, 1, 1, 0, 0, 0, 0, time.UTC)
