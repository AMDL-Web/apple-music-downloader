// Package config owns the service configuration: the Config struct and its
// sections, their defaults (Default), and the YAML file they are stored in.
// EnsureFile bootstraps config.yaml from config.example.yaml in the same
// directory when it is missing. Load reads the file with unknown keys
// rejected, overlays AMDL_* environment variables, normalizes deprecated
// fields, clamps oversized numeric limits and then runs Validate. Store holds
// the live snapshot shared after startup, DownloadOverrides layers per-job
// settings over a base Config, and MutableView, RuntimeLockedChanges and
// EnvLockedChanges back the runtime config API.
//
// Invariants: isRuntimeKey is the single list of fields that may change
// without a restart; Load clamps out-of-range limits read from disk while
// Validate and ApplyValidatedStrict reject them; Save writes owner-only files;
// and the key set of configs/config.example.yaml must equal the keys derived
// from Config's YAML tags (TestCommittedExampleDocumentsEveryConfigKey).
package config
