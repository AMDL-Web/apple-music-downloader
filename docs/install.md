# Installing amdl without Docker

> Type: runbook

`amdl` is one binary: the command line, the MCP server, the terminal interface and
the backend with its web console. This page installs it on macOS, Linux or Windows and
runs the backend on the same machine. To run the backend in Docker instead, see
[deployment.md](deployment.md); `amdl` installed here can still talk to it.

Downloading needs three things on the machine that runs the backend:

1. The `amdl` binary. Releases build it for macOS and Linux on amd64 and arm64 and
   for Windows on amd64 (`DIST_TARGETS` in the `Makefile`).
2. ffmpeg, which writes the final files ([ffmpeg](#ffmpeg)).
3. A wrapper, the service that holds the Apple Music account. It runs natively only
   on Linux, but there are ways to use it from any OS ([The wrapper](#the-wrapper)).

## Install the binary

### macOS and Linux

```bash
curl -fsSL https://raw.githubusercontent.com/AMDL-Web/apple-music-downloader/main/deploy/install/install.sh | sh
```

The script (`deploy/install/install.sh`) downloads the latest release's archive for
the machine, checks it against the release's `SHA256SUMS`, and installs `amdl` into
`~/.local/bin`. It prints the line to add to `PATH` when that directory is not on it,
and the command that installs ffmpeg when ffmpeg is missing. Options go after
`sh -s --`:

| Option | Effect |
| --- | --- |
| `--version v2.0.0` | Install that release instead of the latest |
| `--system` | Install into `/usr/local/bin`; the script says so before it runs `sudo` |
| `--bin-dir DIR` | Install into `DIR` |
| `--service` | Run `amdl service install` afterwards ([Run the backend](#run-the-backend)) |
| `--wrapper URL` | The wrapper address for `--service` |
| `--dry-run` | Print the plan and change nothing |

```bash
curl -fsSL https://raw.githubusercontent.com/AMDL-Web/apple-music-downloader/main/deploy/install/install.sh | sh -s -- --service --wrapper http://nas:30020
```

### Windows

In PowerShell:

```powershell
irm https://raw.githubusercontent.com/AMDL-Web/apple-music-downloader/main/deploy/install/install.ps1 | iex
```

The script (`deploy/install/install.ps1`) does the same as the shell script, installs
`amdl.exe` into `%LOCALAPPDATA%\Programs\amdl` and adds that directory to the user's
`PATH`; terminals opened afterwards find it. To pass options, run it as a script block:

```powershell
& ([scriptblock]::Create((irm https://raw.githubusercontent.com/AMDL-Web/apple-music-downloader/main/deploy/install/install.ps1))) -Service -Wrapper http://nas:30020
```

Its options are `-Version`, `-InstallDir`, `-Service`, `-Wrapper`, `-Force` and
`-DryRun`. Windows on ARM runs the amd64 build under emulation.

### Homebrew and Scoop

Each release renders a Homebrew formula and a Scoop manifest that install `amdl` with
ffmpeg as a dependency (`deploy/packaging/`). They reach users through a tap and a bucket
that are not published yet; once they are:

```bash
brew install amdl-web/tap/amdl
```

```powershell
scoop bucket add amdl https://github.com/AMDL-Web/scoop-bucket
scoop install amdl/amdl
```

Until then, `amdl.rb` and `amdl.json` are attached to each release.

### By hand

Download `amdl_<version>_<os>_<arch>.tar.gz` (`.zip` on Windows) and `SHA256SUMS` from
the [releases](https://github.com/AMDL-Web/apple-music-downloader/releases), check the
archive with `sha256sum -c SHA256SUMS --ignore-missing` (`shasum -a 256` on macOS,
`Get-FileHash` on Windows), and put `amdl` from it on your `PATH`.

## ffmpeg

The backend runs ffmpeg for every track; simulate mode is the one exception. `amdl doctor`
reports whether the machine has it, which version, and the command that installs it
with the package manager it finds:

| System | Command |
| --- | --- |
| macOS | `brew install ffmpeg` |
| Debian, Ubuntu | `sudo apt-get install ffmpeg` |
| Fedora | `sudo dnf install ffmpeg-free` (or `ffmpeg` from RPM Fusion) |
| Arch | `sudo pacman -S ffmpeg` |
| Alpine | `sudo apk add ffmpeg` |
| Windows | `winget install --id Gyan.FFmpeg -e`, or `scoop install ffmpeg` |

`amdl service install` refuses to register the service while ffmpeg is missing, and
records the full path of the one it finds as `tools.ffmpeg`, because a service starts
with a shorter `PATH` than a terminal. Set `tools.ffmpeg` yourself for an ffmpeg
elsewhere. The backend logs a warning at startup without it, and each download fails
with the same install hint.

## The wrapper

The wrapper holds the Apple Music account session and serves what the backend needs to
decrypt ([deployment.md](deployment.md#the-wrapper) describes it and wrapper-manager v2).
amdl only needs its address. Three ways to get one:

- **A wrapper on another machine**, such as a NAS or a Linux server, run as in
  [deployment.md](deployment.md#the-wrapper). Pass its address to `--wrapper`. This is
  the simplest setup for macOS and Windows.
- **On Linux with user namespaces** (`unshare -Ur true` succeeds), run
  `wrapper-lite-rootless` or wrapper-manager v2 natively on the same machine, then use
  `--wrapper http://127.0.0.1:12340` (wrapper-lite's default port).
- **On macOS, Windows, or Linux without user namespaces** (such as Synology DSM), run
  upstream's `wrapper-lite-qemu`, which boots wrapper-lite in a small QEMU guest and
  forwards its port to the host; the
  [wrapper-lite README](https://github.com/WorldObservationLog/wrapper/tree/lite#run-with-qemu-recommended)
  calls it the recommended way to run. wrapper-manager v2 has an equivalent,
  `wrapper-manager-qemu`
  ([its README](https://github.com/WorldObservationLog/wrapper-manager/tree/v2#run-on-any-os-via-qemu-wrapper-manager-qemu)).

Notes on `wrapper-lite-qemu`, from upstream's sources:

- Its packages, with QEMU and its firmware bundled, are build artifacts of the
  `build-lite` workflow on the `lite` branch, not GitHub releases:
  `wrapper-lite-qemu-macos-aarch64`, `-windows-x86_64`, `-linux-x86_64` and
  `-linux-aarch64`. There is none for Intel Macs. GitHub serves artifacts only to
  signed-in users and deletes them after 90 days; wrapper-manager's README downloads them
  through nightly.link, for example
  `https://nightly.link/WorldObservationLog/wrapper/workflows/build-lite.yml/lite/wrapper-lite-qemu-macos-aarch64.zip`.
- The guest is x86_64. The launcher accelerates it with KVM on Linux and WHPX on
  Windows, and falls back to TCG, software emulation, when those are unavailable. On
  Apple Silicon it always uses TCG, since HVF cannot run an x86_64 guest
  ([`wrapper-lite-qemu.cpp`](https://github.com/WorldObservationLog/wrapper/blob/lite/wrapper-lite-qemu.cpp), `autoAccel`).
- Log in once, then serve, as the README shows:
  `./wrapper-lite-qemu --login user:pass --code-from-file`, then `./wrapper-lite-qemu`.
  It listens on `127.0.0.1:12340`, so `--wrapper http://127.0.0.1:12340`.

## Run the backend

As a background service that starts at login, the usual choice on a desktop:

```bash
amdl service install --wrapper http://127.0.0.1:12340
amdl service status
```

`status` prints the URL of the web console and API; the command line, the MCP server and
the terminal interface find the service without `--server`. The service, how it restarts
and how to stop it: [cli.md](cli.md#background-service).

Or for one command at a time, without a service: `amdl --server embedded <command>`
([cli.md](cli.md#running-the-backend-in-amdl)).

Then check everything at once:

```bash
amdl doctor
```

It checks the server, its ffmpeg and its wrapper, and ffmpeg on this machine, and prints
a hint for each failure.

## Where files go

| | macOS | Linux | Windows |
| --- | --- | --- | --- |
| Config, database, log | `~/Library/Application Support/AMDL/server` | `~/.config/AMDL/server` | `%AppData%\AMDL\server` |
| Scratch files | `~/Library/Caches/AMDL` | `~/.cache/AMDL` | `%LocalAppData%\AMDL` |
| Downloads | `~/Music/AMDL` | `~/Music/AMDL` | `%UserProfile%\Music\AMDL` |

These follow `internal/local#DefaultDirs`; Linux honours `XDG_CONFIG_HOME` and
`XDG_CACHE_HOME`. The desktop app uses the same files. `download.downloads_dir` in the
config moves the downloads.

## Upgrade

Run the install script again. It replaces the binary only when the version differs (or
with `--force` / `-Force`) and restarts a running service on the new one. With Homebrew
or Scoop, `brew upgrade amdl` or `scoop update amdl`, then
`amdl service stop` and `amdl service start`.

## Uninstall

```bash
amdl service uninstall
```

Then delete the binary: `~/.local/bin/amdl` (or `/usr/local/bin/amdl`), or
`%LOCALAPPDATA%\Programs\amdl` on Windows, where you also remove that directory from the
user `PATH`. With Homebrew or Scoop, `brew uninstall amdl` or `scoop uninstall amdl`.
Neither step touches the data in [Where files go](#where-files-go); delete those
directories to remove the download history and settings too.
