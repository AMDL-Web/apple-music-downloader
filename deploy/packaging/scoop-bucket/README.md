# AMDL Scoop bucket

```powershell
scoop bucket add amdl https://github.com/AMDL-Web/scoop-bucket
scoop install amdl/amdl
```

`bucket/amdl.json` installs `amdl.exe` from a release of
[AMDL-Web/apple-music-downloader](https://github.com/AMDL-Web/apple-music-downloader),
with ffmpeg from Scoop's main bucket as a dependency. The release workflow there writes
it for every release; change `deploy/packaging/amdl.json.tmpl` in that repository, not
the manifest here.

Setup, the wrapper and the background service:
[docs/install.md](https://github.com/AMDL-Web/apple-music-downloader/blob/main/docs/install.md).
