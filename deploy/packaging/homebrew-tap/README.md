# AMDL Homebrew tap

```sh
brew install amdl-web/tap/amdl
```

`Formula/amdl.rb` installs the `amdl` binary from a release of
[AMDL-Web/apple-music-downloader](https://github.com/AMDL-Web/apple-music-downloader),
with ffmpeg as a dependency. The release workflow there writes it for every release;
change `deploy/packaging/amdl.rb.tmpl` in that repository, not the formula here.

Setup, the wrapper and the background service:
[docs/install.md](https://github.com/AMDL-Web/apple-music-downloader/blob/main/docs/install.md).
