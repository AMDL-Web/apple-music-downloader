package mode

import (
	"path/filepath"

	"github.com/egoist/mygo"
)

// Dirs are the directories the app keeps its files in.
type Dirs struct {
	// Data holds settings.json and, under server/, the embedded backend's
	// config, hooks and database: ~/Library/Application Support/AMDL,
	// %AppData%\AMDL or ~/.config/AMDL.
	Data string
	// Cache holds artwork and, under tmp/, the embedded backend's scratch
	// files, which it can resume a track from after a restart.
	Cache string
	// Logs is where the app writes its log.
	Logs string
	// Music is the user's music folder, the default parent of downloads.
	Music string
	// Resources holds the files the app ships with, such as bin/ffmpeg.
	Resources string
}

// AppDirs returns the directories of the running app. Call it after
// mygo.App.SetName; it creates the data, cache and log directories.
func AppDirs() (Dirs, error) {
	var d Dirs
	for _, p := range []struct {
		name mygo.PathName
		dst  *string
	}{
		{mygo.PathUserData, &d.Data}, {mygo.PathCache, &d.Cache}, {mygo.PathLogs, &d.Logs},
		{mygo.PathMusic, &d.Music}, {mygo.PathResources, &d.Resources},
	} {
		v, err := mygo.App.Path(p.name)
		if err != nil {
			return Dirs{}, err
		}
		*p.dst = v
	}
	return d, nil
}

// ServerDir holds the embedded backend's files.
func (d Dirs) ServerDir() string { return filepath.Join(d.Data, "server") }

// DefaultDownloads is where the standalone backend writes unless the
// settings say otherwise.
func (d Dirs) DefaultDownloads() string { return filepath.Join(d.Music, "AMDL") }

// Downloads resolves the settings' downloads directory.
func (d Dirs) Downloads(s Settings) string {
	if s.DownloadsDir != "" {
		return s.DownloadsDir
	}
	return d.DefaultDownloads()
}
