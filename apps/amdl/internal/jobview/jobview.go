// Package jobview holds the views of a job that the CLI and the MCP server
// print: the summary of one job, its compact row in a list, and the merge of
// stream events into a job's detail.
package jobview

import (
	"bytes"
	"encoding/json"
	"strings"

	"amdl/internal/client"
	"amdl/internal/domain"
	"amdl/internal/format"
	"amdl/internal/progress"
)

// Track is one item in the summary of a job.
type Track struct {
	Index      int    `json:"index"`
	Title      string `json:"title,omitempty"`
	Status     string `json:"status"`
	Quality    string `json:"quality,omitempty"`
	FileSize   int64  `json:"file_size,omitempty"`
	OutputPath string `json:"output_path,omitempty"`
	Error      string `json:"error,omitempty"`
}

// Summary is the default view of a job: the Job without its cover palettes,
// plus its album bar and one short entry per track, about half the size of the
// full detail.
func Summary(d client.DownloadDetail) map[string]any {
	out := jobMap(d.Job)
	for k := range out {
		if isPalette(k) {
			delete(out, k)
		}
	}
	tracks := make([]Track, 0, len(d.Items))
	for _, it := range d.Items {
		tracks = append(tracks, Track{
			Index: it.Index, Title: it.Title, Status: string(it.Status),
			Quality:  format.Quality(it.Codec, it.BitDepth, it.SampleRate, it.Bitrate),
			FileSize: it.FileSize, OutputPath: it.OutputPath, Error: it.Error,
		})
	}
	out["album_progress"] = progress.AlbumProgress(d.Job.Status, d.Items)
	out["tracks"] = tracks
	return out
}

// isPalette reports whether a key is one of the artwork colors of Apple's
// catalog or of a job: artwork_bg_color, artwork_text_color1 to 4.
func isPalette(key string) bool {
	return strings.Contains(key, "_bg_color") || strings.Contains(key, "_text_color")
}

// DropPalette removes the artwork color keys from raw JSON at any depth, and
// returns raw unchanged when it is not JSON.
func DropPalette(raw json.RawMessage) json.RawMessage {
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.UseNumber()
	var v any
	if dec.Decode(&v) != nil {
		return raw
	}
	out, err := json.Marshal(dropPalette(v))
	if err != nil {
		return raw
	}
	return out
}

func dropPalette(v any) any {
	switch t := v.(type) {
	case map[string]any:
		for k, x := range t {
			if isPalette(k) {
				delete(t, k)
				continue
			}
			t[k] = dropPalette(x)
		}
	case []any:
		for i, x := range t {
			t[i] = dropPalette(x)
		}
	}
	return v
}

// rowKeys are the Job fields a list row keeps.
var rowKeys = []string{"id", "type", "status", "title", "artist_name", "curator_name", "input",
	"total_items", "completed_items", "skipped_items", "failed_items", "cancelled_items",
	"error", "created_at", "finished_at"}

// Row is a job cut down to what a list needs.
func Row(j domain.Job) map[string]any {
	full := jobMap(j)
	out := make(map[string]any, len(rowKeys))
	for _, k := range rowKeys {
		if v, ok := full[k]; ok {
			out[k] = v
		}
	}
	return out
}

func jobMap(j domain.Job) map[string]any {
	raw, _ := json.Marshal(j) // a domain.Job always marshals
	var out map[string]any
	_ = json.Unmarshal(raw, &out) // and decodes back into a map
	return out
}

// Apply merges a stream event's payload into a job's detail: lifecycle events
// carry the Job's fields, item events the JobItem's. index maps item ids to
// their place in d.Items and gains new items.
func Apply(d *client.DownloadDetail, index map[string]int, ev domain.Event) {
	if len(ev.Payload) == 0 {
		return
	}
	if ev.ItemID == "" {
		// Job-level events whose payload is the job's snapshot (lifecycle,
		// resolved_input) carry its id; the others carry none.
		var probe struct {
			ID string `json:"id"`
		}
		if json.Unmarshal(ev.Payload, &probe) == nil && probe.ID == d.Job.ID {
			_ = json.Unmarshal(ev.Payload, &d.Job) // a malformed payload leaves the last snapshot
		}
		return
	}
	i, ok := index[ev.ItemID]
	if !ok {
		var it domain.JobItem
		if json.Unmarshal(ev.Payload, &it) != nil || it.ID == "" {
			return
		}
		index[it.ID] = len(d.Items)
		d.Items = append(d.Items, it)
		return
	}
	_ = json.Unmarshal(ev.Payload, &d.Items[i]) // as above
}

// Index maps the items of a detail by id, for Apply.
func Index(d client.DownloadDetail) map[string]int {
	m := make(map[string]int, len(d.Items))
	for i, it := range d.Items {
		m[it.ID] = i
	}
	return m
}
