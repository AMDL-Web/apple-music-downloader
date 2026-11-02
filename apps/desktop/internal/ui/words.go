package ui

import (
	"strconv"
	"strings"
	"time"

	"amdl/apps/desktop/internal/store"
	"amdl/internal/client"
	"amdl/internal/domain"
	"amdl/internal/format"
)

// words keeps what one function made of each input, so a frame showing the
// same text reads it instead of making it again: a list made every row's
// subtitle, count and quality every frame. Its key holds everything the
// text says, and build reads nothing else. It starts afresh once it holds
// wordsMax inputs, so inputs that come and go, as a running job's count,
// do not pile up.
type words[K comparable] struct{ m map[K]string }

const wordsMax = 2048

func (w *words[K]) of(k K, build func(K) string) string {
	if s, ok := w.m[k]; ok {
		return s
	}
	if w.m == nil || len(w.m) >= wordsMax {
		w.m = make(map[K]string)
	}
	s := build(k)
	w.m[k] = s
	return s
}

// texts are the App's words, one per kind of text.
type texts struct {
	rowSub   words[rowSubKey]
	rowCount words[rowCountKey]
	quality  words[qualityKey]
	hitKey   words[hitID]
	hitFrom  words[hitID]
	facts    words[tileFactsKey]
	songs    words[hitFactsKey]
	hints    words[hintsKey]
	length   words[int]
	trackSub words[trackSubKey]
	gridKey  words[gridKey]
}

type rowSubKey struct {
	by, typ, title string
	finished       time.Time
}

// jobSubtitle is rowSubtitle, made once for what it says.
func (a *App) jobSubtitle(j *store.Job) string {
	k := rowSubKey{by: j.By(), typ: j.Type, title: j.Title}
	if j.Status == domain.JobCompleted && j.FinishedAt != nil {
		k.finished = *j.FinishedAt
	}
	return a.texts.rowSub.of(k, func(k rowSubKey) string {
		j := store.Job{Job: domain.Job{ArtistName: k.by, Type: k.typ, Title: k.title, Status: domain.JobQueued}}
		if !k.finished.IsZero() {
			j.Status, j.FinishedAt = domain.JobCompleted, &k.finished
		}
		return rowSubtitle(&j)
	})
}

type rowCountKey struct {
	total, done, percent int
	running              bool
}

// jobCount is rowCount, made once for what it says.
func (a *App) jobCount(j *store.Job) string {
	k := rowCountKey{total: j.TotalItems, done: j.Done(), running: j.Status == domain.JobRunning}
	if k.running {
		k.percent = j.Percent()
	}
	return a.texts.rowCount.of(k, func(k rowCountKey) string {
		j := store.Job{Job: domain.Job{TotalItems: k.total, CompletedItems: k.done, Status: domain.JobQueued}, Progress: float64(k.percent) / 100}
		if k.running {
			j.Status = domain.JobRunning
		}
		return rowCount(&j)
	})
}

type qualityKey struct {
	codec                string
	depth, rate, bitrate int
}

// jobQualityText is jobQuality, made once for what it says.
func (a *App) jobQualityText(j *store.Job) string {
	for i := range j.Items {
		if it := &j.Items[i]; it.Codec != "" {
			return a.qualityText(it.Codec, it.BitDepth, it.SampleRate, it.Bitrate)
		}
	}
	return ""
}

func (a *App) qualityText(codec string, depth, rate, bitrate int) string {
	return a.texts.quality.of(qualityKey{codec: codec, depth: depth, rate: rate, bitrate: bitrate}, func(k qualityKey) string {
		return format.Quality(k.codec, k.depth, k.rate, k.bitrate)
	})
}

type hitID struct{ typ, id string }

// hitKey is store.HitKey, made once for each hit.
func (a *App) hitKey(h client.SearchHit) string {
	return a.texts.hitKey.of(hitID{h.Type, h.ID}, func(k hitID) string {
		return store.HitKey(client.SearchHit{Type: k.typ, ID: k.id})
	})
}

// hitFrom is the name a flight from a hit's tile goes by ("hit:" and its
// key), made once for each hit.
func (a *App) hitFrom(h client.SearchHit) string {
	return a.texts.hitFrom.of(hitID{h.Type, h.ID}, func(k hitID) string {
		return "hit:" + store.HitKey(client.SearchHit{Type: k.typ, ID: k.id})
	})
}

type tileFactsKey struct{ typ, genre, artist, name, date string }

// tileLine is a tile's facts joined, made once for what they say.
func (a *App) tileLine(h client.SearchHit) string {
	return a.texts.facts.of(tileFactsKey{h.Type, h.Genre, h.ArtistName, h.Name, h.ReleaseDate}, func(k tileFactsKey) string {
		return strings.Join(tileFacts(client.SearchHit{Type: k.typ, Genre: k.genre, ArtistName: k.artist, Name: k.name, ReleaseDate: k.date}), " · ")
	})
}

type hitFactsKey struct {
	artist, album string
	ms            int
}

// songLine is a song card's facts joined, made once for what they say.
func (a *App) songLine(h client.SearchHit) string {
	return a.texts.songs.of(hitFactsKey{h.ArtistName, h.AlbumName, h.DurationMS}, func(k hitFactsKey) string {
		return strings.Join(hitFacts(client.SearchHit{ArtistName: k.artist, AlbumName: k.album, DurationMS: k.ms}), " · ")
	})
}

// hintsKey is a hit's traits, as many as a hint reads, and whether its
// tile is small.
type hintsKey struct {
	t     [4]string
	n     int
	small bool
}

// tileHints is shortHints, made once for each set of traits.
func (a *App) tileHints(h client.SearchHit, size float32) string {
	if len(h.AudioTraits) == 0 {
		return ""
	}
	k := hintsKey{n: min(len(h.AudioTraits), len(hintsKey{}.t)), small: size < 180}
	copy(k.t[:], h.AudioTraits)
	return a.texts.hints.of(k, func(k hintsKey) string {
		size := float32(180)
		if k.small {
			size = 0
		}
		return shortHints(k.t[:k.n], size)
	})
}

// songLength is a song's length, "" where it is not known (ms 0), made
// once for each length.
func (a *App) songLength(ms int) string {
	if ms <= 0 {
		return ""
	}
	return a.texts.length.of(ms, func(ms int) string { return format.Duration(time.Duration(ms) * time.Millisecond) })
}

type trackSubKey struct {
	codec                string
	depth, rate, bitrate int
	status               domain.ItemStatus
	attempt, attempts    int
	size                 int64
	message              string
}

// trackSubText is trackSub, made once for what it says.
func (a *App) trackSubText(it *domain.JobItem) string {
	k := trackSubKey{codec: it.Codec, depth: it.BitDepth, rate: it.SampleRate, bitrate: it.Bitrate, status: it.Status,
		attempt: it.Attempt, attempts: it.MaxAttempts, size: it.FileSize, message: it.StatusMessage}
	return a.texts.trackSub.of(k, func(k trackSubKey) string {
		return trackSub(&domain.JobItem{Codec: k.codec, BitDepth: k.depth, SampleRate: k.rate, Bitrate: k.bitrate, Status: k.status,
			Attempt: k.attempt, MaxAttempts: k.attempts, FileSize: k.size, StatusMessage: k.message})
	})
}

type gridKey struct {
	group string
	i     int
}

// tileKey is the key of a grid's i'th tile, made once.
func (a *App) tileKey(group string, i int) string {
	return a.texts.gridKey.of(gridKey{group, i}, func(k gridKey) string { return k.group + strconv.Itoa(k.i) })
}
