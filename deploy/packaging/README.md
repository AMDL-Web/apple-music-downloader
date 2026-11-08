# Packaging

Package-manager definitions for the `amdl` binary that every GitHub release
attaches (`make dist`):

- `amdl.rb.tmpl`: the Homebrew formula, for macOS and Linux.
- `amdl.json.tmpl`: the Scoop manifest, for Windows.

Both declare ffmpeg as a dependency, so a package-manager install brings it
along. For each release the `binaries` job in `.github/workflows/release.yml`
renders them with `render.mjs` from the release's `SHA256SUMS` and attaches
`amdl.rb` and `amdl.json` to the release next to the archives.

## The tap and the bucket

Users install from two repositories that do not exist yet:

| Repository | Holds | Users run |
| --- | --- | --- |
| `AMDL-Web/homebrew-tap` | `Formula/amdl.rb` | `brew install amdl-web/tap/amdl` |
| `AMDL-Web/scoop-bucket` | `bucket/amdl.json` | `scoop bucket add amdl https://github.com/AMDL-Web/scoop-bucket` then `scoop install amdl/amdl` |

`homebrew-tap/README.md` and `scoop-bucket/README.md` here are their READMEs.

The `packaging` job in the release workflow copies the rendered files into both
repositories. It is off until the maintainer sets it up once:

1. Create the two public repositories under `AMDL-Web`, each with the README from
   this directory.
2. Create a fine-grained personal access token limited to those two
   repositories with *Contents: read and write*, and add it to this repository
   as the Actions secret `PACKAGING_TOKEN`.
3. Set the repository variable `PACKAGING_PUBLISH` to `true`.

Until then, a release still attaches `amdl.rb` and `amdl.json`, which can be
copied into the repositories by hand.
