# Benchmark corpus

A kept set of albums and playlists for re-measuring download performance, and the
scripts that recorded it. Work in progress, parked for 2.0.1: the head-to-head with
zhaarey/apple-music-downloader and WorldObservationLog/AppleMusicDecrypt (v3) has
not run yet.

## Files

- `corpus.json`: 26 items (20 albums, 5 playlists, one library-only playlist as its
  song links; 738 tracks), with formats, shapes, the codecs to fetch and the
  `compare` subset. Formats were read with `POST /api/v1/quality` on each album's
  first track (`probe.py`). `not_covered` lists what the set lacks and why.
- `capture-index.tsv`: size and cache key of every response recorded by
  `tools/mediareplay` in record mode (amp-api catalog reads with their query, CDN
  media by path). Audio and tokens are not kept here; the cache itself lives on the
  test host.
- `record-pass-1.jsonl`: the first record pass, one line per item and codec.
- `rec-amdl.sh`: downloads every corpus item and codec through one amdl.
- `amdl-up.sh`: a throwaway amdl behind mediareplay (`HTTPS_PROXY`,
  `SSL_CERT_FILE` with the proxy CA). The wrapper stays live on loopback.
- `run1.sh`: one measured run of `amdl`, `zhaarey` or `amd` in a fresh directory and
  a fresh `systemd-run --user` unit: wall time, CPU time, cgroup memory peak, anon
  peak, and the proxy's misses. `amdl-one.sh` runs `amdl serve` and one download in
  that unit; `amd-run.py` drives AppleMusicDecrypt v3 without its TUI.

The scripts carry the test host's paths and ports (wrapper copy on 12360, proxy on
18700); parameterize them before reuse.

## State

- Record pass 1 done: 32 of 34 jobs completed, 24 GiB of media. The two AAC-only EPs
  failed with `no enhanced hls manifest` because the pass ran with
  `--codec-fallback=false`, so AAC-LC was never tried; rerun them with fallback.
- A second codec on an album already on disk answers `already_downloaded`; record
  `ec3`, `aac-binaural` and `aac-downmix` on a fresh amdl per codec
  (`amdl library download albums <id> --codec <c>` on the same one).
- The library-only playlist's song links returned no job in pass 1; not yet checked.
- Left: record pass 2 (fills files a resumed transfer left out), one record pass
  per competitor (their catalog reads differ), then replay runs of the `compare`
  subset in alternating order, and correctness checks (`ffmpeg -v error` decode,
  per-track audio packet SHA-256 across tools).
