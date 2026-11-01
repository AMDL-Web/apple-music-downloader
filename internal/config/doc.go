// Package config owns the service configuration: the Config struct and its
// sections, their defaults (Default), and the YAML file they are stored in.
// EnsureFile bootstraps a missing config.yaml from the example it is given
// (configs.Example, embedded from configs/config.example.yaml), with any
// seeds (amdl serve --init) set in it. Load reads the file with unknown keys
// rejected, clamps oversized numeric limits and then runs Validate; no
// environment variable overrides a key, and UnreadEnv names the AMDL_*
// variables that are not read. Store holds the live snapshot shared after
// startup (Reload applies hand edits, UpdateAndSave persists API changes,
// Saved and PendingRestart report the changes waiting for a restart),
// DownloadOverrides layers a submission's quality and overwrite choices over
// a base Config, and MutableView and RuntimeLockedChanges back the runtime
// config API.
//
// Invariants: classOf is the single list of which keys apply at once
// (runtime), which the API saves for the next start (restart), and which
// change only in the file (server.listen, database.path, tools.ffmpeg); the
// snapshot holds the values the process runs with, so restart and file keys
// keep their startup values until a restart; PUT and Load share Validate, so a
// value PUT accepts loads on the next start; Load clamps out-of-range limits
// read from disk while Validate rejects them; written config files are
// owner-only; Default equals the values configs/config.example.yaml sets
// except the album and artist path formats (TestDefaultMatchesExample); and
// the example's key set equals the keys derived from Config's YAML tags
// (TestCommittedExampleDocumentsEveryConfigKey).
package config
