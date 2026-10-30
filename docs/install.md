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
| `--force` | Reinstall even when that version is already installed |
| `--dry-run` | Print the plan and change nothing |

```bash
curl -fsSL https://raw.githubusercontent.com/AMDL-Web/apple-music-downloader/main/deploy/install/install.sh | sh -s -- --service --wrapper http://nas:30020
```

### Windows

In PowerShell. The first statement turns on TLS 1.2, which Windows PowerShell 5.1 may
not offer by default and GitHub requires:

```powershell
[Net.ServicePointManager]::SecurityProtocol = [Net.ServicePointManager]::SecurityProtocol -bor 3072; irm https://raw.githubusercontent.com/AMDL-Web/apple-music-downloader/main/deploy/install/install.ps1 | iex
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
from 2.0.1 on; 2.0.0 installs with the scripts above or Docker. Once they are published:

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

Use ffmpeg 8 or later (`internal/media#RecommendedFFmpegMajor`). The audio is the same
with older ones, but ffmpeg 7.0.2 wrote a 24-bit/96 kHz ALAC track's duration without its
last, partial frame, about 16 ms short of the catalog's; `amdl doctor` says when the
ffmpeg it finds is older. The Docker image ships a current one.

`amdl service install` refuses to register the service while ffmpeg is missing, and
records the full path of the one it finds as `tools.ffmpeg`, because a service starts
with a shorter `PATH` than a terminal. Set `tools.ffmpeg` yourself for an ffmpeg
elsewhere. The backend logs a warning at startup without it, and each download fails
with the same install hint.

## Try it without a wrapper

Simulate mode reads the real catalog and selects real media, then fakes the transfer,
decryption and file writes, so it needs neither a wrapper nor ffmpeg
([configuration.md](configuration.md#simulate-mode)). In an empty directory:

```bash
amdl serve
```

The first start writes `configs/config.yaml` and `data/` into the current directory and
serves the API and web console on `http://127.0.0.1:18080`; its ffmpeg warning does not
apply to simulated downloads. In a second terminal:

```bash
export AMDL_URL=http://127.0.0.1:18080
amdl config set simulate.enabled=true
amdl download <album or song link> --wait
```

For another port, start it with `AMDL_SERVER_LISTEN=127.0.0.1:18081 amdl serve` and
set `AMDL_URL` to match. In PowerShell, set each variable on a line of its own, such as
`$env:AMDL_URL = 'http://127.0.0.1:18080'`. `serve --listen` works with `--local` only;
a plain `amdl serve` listens on `server.listen` from its config or `AMDL_SERVER_LISTEN`.
Change other settings with `amdl config set` too. Stop it with Ctrl-C and delete the
directory when done.

## The wrapper

The wrapper holds the Apple Music account session and serves what the backend needs to
decrypt ([deployment.md](deployment.md#the-wrapper) describes it and wrapper-manager v2).
amdl only needs its address. Four ways to get one:

- **A wrapper on another machine**, such as a NAS or a Linux server, run as in
  [deployment.md](deployment.md#the-wrapper). Pass its address to `--wrapper`. This is
  the simplest setup for macOS and Windows.
- **On Linux with user namespaces** (`unshare -Ur true` succeeds), run
  `wrapper-lite-rootless` or wrapper-manager v2 natively on the same machine, then use
  `--wrapper http://127.0.0.1:12340` (wrapper-lite's default port).
- **Let amdl run it** in a QEMU guest on this machine: [A wrapper amdl runs](#a-wrapper-amdl-runs).
  This is the self-contained setup for macOS and Windows.
- **On macOS, Windows, or Linux without user namespaces** (such as Synology DSM), run
  upstream's `wrapper-lite-qemu` yourself, which boots wrapper-lite in a small QEMU guest
  and forwards its port to the host; the
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

### A wrapper amdl runs

```bash
amdl wrapper install          # QEMU, then upstream's guest
amdl service install          # runs the backend, which runs the wrapper
amdl wrapper login            # asks for the Apple ID, password and any two-step code
```

`amdl wrapper install` needs QEMU (`qemu-system-x86_64`). Without it, it offers to run
`brew install qemu` on macOS or `winget install --id SoftwareFreedomConservancy.QEMU -e`
on Windows (pass `--yes` when no one can answer), and prints the package command on
Linux, where it needs `sudo`; `--qemu PATH` names one elsewhere. It then downloads
upstream's guest: the kernel, initramfs and empty disk image of the `qemu-assets`
artifact of upstream's `build-lite` workflow, through nightly.link, and installs it
only if its sha256 matches the pin amdl carries (`internal/wrapperhost#Builtin`) or
the newer one published at `internal/wrapperhost#ManifestURL`. Upstream's artifacts
expire 90 days after their build; when no pin can be downloaded, fetch the artifact
yourself and run `amdl wrapper import <zip|dir>`, which also accepts one of
upstream's `wrapper-lite-qemu` packages. Nothing upstream builds is shipped with amdl:
the guest holds Apple's Android libraries.

The backend runs the guest whenever it runs on the local files with this machine's
wrapper installed (the service, `amdl serve --local` and the desktop app; not a
per-command `--server embedded`), points `wrapper.address` at it, restarts it after a
crash and stops it with itself. Its port is on `127.0.0.1` only. Log in from the
console's Account page, the terminal interface's `/login`, or `amdl wrapper login`;
`amdl wrapper status` shows its `managed` state, `amdl wrapper logs` its console with
secrets redacted, and `amdl wrapper stop` / `start` turn it off and on.

| Host | Acceleration |
| --- | --- |
| Linux | KVM when `/dev/kvm` opens for you (the `kvm` group), else TCG |
| Windows x64 | WHPX, which needs the *Windows Hypervisor Platform* feature, else TCG |
| Intel Mac | HVF |
| Apple Silicon Mac | TCG: the guest is x86_64, which HVF cannot run there |
| Windows on ARM | not supported |

TCG is plain emulation. On a Linux PC it booted the guest in about 12 seconds and
answered `/status` in 2 ms. On an M4 Pro Mac it booted in about 11 seconds and served
again about 25 seconds after a login; a `/key` took 0.52 s at the median and 0.73 s at
p95 (1.69 s for the first after a boot), and a 12-track ALAC 24-bit/96 kHz album of
1011 MB downloaded in 65 seconds. QEMU used 16% of the CPU on average while downloading
(44% at peak, about 7% idle) and at most about 176 MiB of memory.

Known limits, all upstream's:

- A login's password reaches the guest in a 0600 file that is deleted once the guest
  has read it, never on a command line. The guest copies it to its own disk, where deleting
  the file leaves its blocks, so amdl zeroes every copy in `data.img` before the boot
  that follows each login, and once when the backend starts; `managed.login_residue`
  in `amdl wrapper status` is true until then. The disk, `data.img` in the data directory's `wrapper/`, holds the
  account's tokens: it stays owner-only and survives `amdl wrapper update`.
- wrapper-lite does not verify Apple's TLS certificates
  ([upstream issue #75](https://github.com/WorldObservationLog/wrapper/issues/75)).
- One account per machine.

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
| [A wrapper amdl runs](#a-wrapper-amdl-runs), with the account's disk | `~/Library/Application Support/AMDL/wrapper` | `~/.config/AMDL/wrapper` | `%AppData%\AMDL\wrapper` |
| Scratch files | `~/Library/Caches/AMDL` | `~/.cache/AMDL` | `%LocalAppData%\AMDL` |
| Downloads | `~/Music/AMDL` | `~/Music/AMDL` | `%UserProfile%\Music\AMDL` |
| Saved servers (`amdl context`) | `~/Library/Application Support/amdl/cli.yaml` | `~/.config/amdl/cli.yaml` | `%AppData%\amdl\cli.yaml` |

These follow `internal/local#DefaultDirs`; Linux honours `XDG_CONFIG_HOME` and
`XDG_CACHE_HOME`. The desktop app uses the same files. `download.downloads_dir` in the
config moves the downloads. `data.img` in `wrapper/` holds the Apple Music account's
tokens.

## Upgrade

Run the install script again. It replaces the binary only when the version differs (or
with `--force` / `-Force`). A service that was running is stopped for the swap and
started on the new binary; if that fails, the script puts the previous binary back,
starts the service on it and exits with an error. A service you stopped stays stopped.
The service runs the stable path, `opt/amdl` for Homebrew and `current` for Scoop, so
package-manager upgrades keep it valid. With Homebrew
or Scoop, `brew upgrade amdl` or `scoop update amdl`, then
`amdl service stop` and `amdl service start`.

## Uninstall

```bash
amdl service uninstall
amdl wrapper uninstall --purge
```

The first stops the backend, and with it the wrapper amdl runs, and removes the
registration. The second, needed only with [A wrapper amdl runs](#a-wrapper-amdl-runs),
deletes its directory, the account's disk included, which logs the account out for
good; without `--purge` it keeps `data.img` for a later `amdl wrapper install`.

Then delete the binary: `~/.local/bin/amdl` (or `/usr/local/bin/amdl`), or
`%LOCALAPPDATA%\Programs\amdl` on Windows, where you also remove that directory from the
user `PATH`. With Homebrew or Scoop, `brew uninstall amdl` or `scoop uninstall amdl`.
QEMU stays installed when `amdl wrapper install` installed it; remove it with
`brew uninstall qemu` or `winget uninstall --id SoftwareFreedomConservancy.QEMU -e` if
nothing else uses it. The rest of [Where files go](#where-files-go) stays too; delete
those directories to remove the download history and settings.
