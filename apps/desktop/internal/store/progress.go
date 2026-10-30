package store

import (
	"time"

	"amdl/internal/domain"
)

// Stage is one segment of the weighted stage bar.
type Stage int

// The pipeline's stages in order. The backend reports a fraction for
// download and decrypt and a done flag for the rest (domain.ItemProgress).
const (
	StageResolve Stage = iota
	StageDownload
	StageDecrypt
	StageRemux
	StageVerify
	StageTag
	StageSave
	StageCount
)

// StageWeights are the segment widths of the stage bar, in percent: roughly
// the share of a track's wall time each stage takes. The backend deliberately
// sends no overall percentage (domain.ItemProgress), so this weighting is the
// app's own.
var StageWeights = [StageCount]float64{4, 56, 26, 6, 3, 3, 2}

// StageLabels are the stage bar's legend, short enough for its narrow
// segments.
var StageLabels = [StageCount]string{"R", "Download", "Decrypt", "M", "V", "T", "S"}

// Stages is how far each stage has come, each in [0,1].
type Stages [StageCount]float64

// Weighted returns the overall fraction the stages stand for under
// StageWeights.
func (s Stages) Weighted() float64 {
	total := 0.0
	for i, w := range StageWeights {
		total += w * s[i]
	}
	return total / 100
}

// Current returns the first stage that is not done, or StageCount when all
// are.
func (s Stages) Current() Stage {
	for i, v := range s {
		if v < 1 {
			return Stage(i)
		}
	}
	return StageCount
}

// statusRank orders the item statuses of a run, so a stage counts as done
// once the item has moved past it even when its flag is false: Verified stays
// false when download.check_integrity is off, and an unmeasurable transfer
// never reports a fraction.
var statusRank = map[domain.ItemStatus]int{
	domain.ItemQueued:          0,
	domain.ItemResolving:       1,
	domain.ItemWaitingDownload: 2,
	domain.ItemDownloading:     3,
	domain.ItemWaitingDecrypt:  4,
	domain.ItemDecrypting:      5,
	domain.ItemRemuxing:        6,
	domain.ItemTagging:         7,
	domain.ItemSaving:          8,
	domain.ItemCompleted:       9,
}

// ItemStages returns how far one track has come. A completed or
// skipped_existing track is done in every stage: a skipped one reports only
// Resolved because its file was already on disk. A failed or cancelled track
// keeps the stages it reached.
func ItemStages(it domain.JobItem) Stages {
	if it.Status == domain.ItemCompleted || it.Status == domain.ItemSkipped {
		return Stages{1, 1, 1, 1, 1, 1, 1}
	}
	p := it.Progress
	rank, ok := statusRank[it.Status]
	if !ok {
		rank = -1 // failed or cancelled: flags only
	}
	done := func(flag bool, past domain.ItemStatus) float64 {
		if flag || (rank >= 0 && rank >= statusRank[past]) {
			return 1
		}
		return 0
	}
	frac := func(v float64, past domain.ItemStatus) float64 {
		if rank >= 0 && rank >= statusRank[past] {
			return 1
		}
		return min(max(v, 0), 1)
	}
	return Stages{
		StageResolve:  done(p.Resolved, domain.ItemWaitingDownload),
		StageDownload: frac(p.Download, domain.ItemWaitingDecrypt),
		StageDecrypt:  frac(p.Decrypt, domain.ItemRemuxing),
		StageRemux:    done(p.Remuxed, domain.ItemTagging),
		StageVerify:   done(p.Verified, domain.ItemTagging),
		StageTag:      done(p.Tagged, domain.ItemSaving),
		StageSave:     done(p.Saved, domain.ItemCompleted),
	}
}

// Indeterminate reports whether a track is in a measured stage that has no
// fraction yet: downloading or decrypting at 0, which means "running, size
// unknown" rather than "no bytes yet".
func Indeterminate(it domain.JobItem) bool {
	switch it.Status {
	case domain.ItemDownloading:
		return it.Progress.Download == 0
	case domain.ItemDecrypting:
		return it.Progress.Decrypt == 0
	}
	return false
}

// jobStages averages the stages of a job's tracks; tracks the job has not
// listed yet count as not started. Without items it falls back to the job's
// counters, which are all a feed snapshot carries.
func jobStages(j domain.Job, items []domain.JobItem) Stages {
	var s Stages
	total := max(j.TotalItems, len(items))
	if total == 0 {
		if j.Status == domain.JobCompleted {
			return Stages{1, 1, 1, 1, 1, 1, 1}
		}
		return s
	}
	if len(items) == 0 {
		done := float64(j.CompletedItems+j.SkippedItems) / float64(total)
		for i := range s {
			s[i] = done
		}
		return s
	}
	for _, it := range items {
		is := ItemStages(it)
		for i := range s {
			s[i] += is[i]
		}
	}
	for i := range s {
		s[i] /= float64(total)
	}
	return s
}

// rateWindow is how far back the progress samples behind the remaining-time
// estimate reach.
const rateWindow = 30 * time.Second

type sample struct {
	at       time.Time
	progress float64
}

// addSample records a job's progress and drops samples older than the
// window, keeping the oldest one inside it as the rate's base.
func addSample(samples []sample, at time.Time, progress float64) []sample {
	if n := len(samples); n > 0 && progress < samples[n-1].progress {
		samples = samples[:0] // a retry went back; the old rate means nothing
	}
	samples = append(samples, sample{at, progress})
	cut := 0
	for cut < len(samples)-2 && at.Sub(samples[cut+1].at) > rateWindow {
		cut++
	}
	return samples[cut:]
}

// remaining estimates the time left from the progress rate over the window,
// or 0 when there is not enough to go on.
func remaining(samples []sample) time.Duration {
	if len(samples) < 2 {
		return 0
	}
	first, last := samples[0], samples[len(samples)-1]
	dt := last.at.Sub(first.at)
	dp := last.progress - first.progress
	if dt < 3*time.Second || dp <= 0 || last.progress >= 1 {
		return 0
	}
	return time.Duration(float64(dt) * (1 - last.progress) / dp)
}
