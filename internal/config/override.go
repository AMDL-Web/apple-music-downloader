package config

// DownloadOverrides holds the per-add quality choices. An add may attach one;
// it is stored on the record (and on a station run) and applied over the
// runtime config each time a run starts, so it survives retries and restarts
// without leaking into other downloads.
//
// Every field is optional: a nil field keeps the runtime config's value. The
// JSON keys are named after the runtime download.* config keys they override
// (TestDownloadOverridesCoverRuntimeDownloadKeys). Every other runtime
// download key is deliberately absent and comes only from the runtime config
// (overrideExclusions): directories, path templates, lyrics, covers,
// max_attempts, give_up_after_runs, check_integrity and integrity_mode are install
// settings that Settings changes for every job, and progress_event_interval_ms
// shapes the event stream every subscriber of a job shares. max_running_jobs
// and max_parallel_downloads are restart keys that size process-wide pools.
//
// QualityPriority is *[]string rather than []string so an explicit empty list
// survives the JSON round trip through the store; Validate then rejects it
// like an empty download.quality_priority. Rows stored before the other keys
// were removed (force_overwrite among them) decode with those keys ignored.
type DownloadOverrides struct {
	QualityPriority   *[]string `json:"quality_priority,omitempty"`
	CodecAlternative  *bool     `json:"codec_alternative,omitempty"`
	ALACMaxSampleRate *int      `json:"alac_max_sample_rate,omitempty"`
	ALACMaxBitDepth   *int      `json:"alac_max_bit_depth,omitempty"`
}

// Apply returns base with every non-nil override substituted. Nil-safe: a nil
// receiver returns base unchanged.
func (o *DownloadOverrides) Apply(base Config) Config {
	if o == nil {
		return base
	}
	d := &base.Download
	if o.QualityPriority != nil {
		d.QualityPriority = *o.QualityPriority
	}
	if o.CodecAlternative != nil {
		d.CodecAlternative = *o.CodecAlternative
	}
	if o.ALACMaxSampleRate != nil {
		d.ALACMaxSampleRate = *o.ALACMaxSampleRate
	}
	if o.ALACMaxBitDepth != nil {
		d.ALACMaxBitDepth = *o.ALACMaxBitDepth
	}
	return base
}

// ApplyValidated applies the overrides and checks the result against the
// rules the runtime config itself must satisfy. The submit handler calls it
// on fresh input; a job calls it again each time it runs, against the
// runtime config of that moment.
func (o *DownloadOverrides) ApplyValidated(base Config) (Config, error) {
	applied := o.Apply(base)
	if err := applied.Validate(); err != nil {
		return applied, err
	}
	return applied, nil
}
