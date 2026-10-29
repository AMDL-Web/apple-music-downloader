// Package config owns the service configuration: the Config struct and its
// sections, their defaults (Default), and the YAML file they are stored in.
// EnsureFile bootstraps config.yaml from config.example.yaml in the same
// directory when it is missing. Load reads the file with unknown keys
// rejected, overlays the three AMDL_* environment overrides, clamps oversized
// numeric limits and then runs Validate; UnreadEnv names the AMDL_* variables
// it ignores. Store holds the live snapshot shared after
// startup (Reload applies hand edits, UpdateAndSave persists API changes),
// DownloadOverrides layers a submission's quality and overwrite choices over
// a base Config, and MutableView and RuntimeLockedChanges back the runtime
// config API.
//
// Invariants: isRuntimeKey is the single list of fields that may change
// without a restart; Load clamps out-of-range limits read from disk while
// Validate rejects them; written config files are owner-only; Default equals the values configs/config.example.yaml sets
// except the album and artist path formats (TestDefaultMatchesExample); and
// the example's key set equals the keys derived from Config's YAML tags
// (TestCommittedExampleDocumentsEveryConfigKey).
package config
