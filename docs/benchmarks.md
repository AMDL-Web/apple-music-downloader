# Benchmarks

> Type: record
>
> Verified at 72994e3 on 2026-11-01 (memory modes after in-process decryption) and at
> a6a6b46 on 2026-08-10 (the other sections); snapshot, not maintained.

Three measurements: the two memory modes end to end after 2.0 moved decryption into the
backend, and from 2026-08 a post-decrypt micro-benchmark that isolates the
flatten/verify/tag path and an end-to-end run with `wrapper-manager` decrypting.

## Memory modes after in-process decryption

Measured on 2026-11-01 at `72994e3`, where the backend decrypts in-process and sizes its
decrypt pool to `GOMAXPROCS`.

- The two albums of the 2026-08 run were recorded once through
  [`tools/mediareplay`](../tools/mediareplay/main.go) and replayed into a fresh
  `amdl serve` process with an empty queue for every unit. The wrapper stayed live:
  wrapper-lite on rog, `wrapper-manager` on the NAS.
- A unit counts only if every track was ALAC on its first attempt and no media came from
  Apple. wrapper-lite stops serving keys after about 1024 per start, so the wrapper was
  restarted between batches, and the units it failed were discarded and run again.
- Matrix: four rounds per album, `max_parallel_downloads` 4 and 16, low and high in ABBA
  order. The other scenarios ran one to three rounds at 16. They were the proxy capping
  total bandwidth at 12 MB/s, both albums submitted together, `temp_dir` on a slow volume,
  and a memory limit on the process with swap off.
- Time runs from submission to the last job finishing. Anon is the peak of the cgroup's
  anonymous memory (page cache excluded), sampled every 0.1 s on rog and 0.2 s on the NAS.
  Temp is the peak size of `temp_dir`. Written is the process's block-device writes,
  final files included. On rog's Windows volume these miss the 9p writes.
- Per-track audio packet SHA-256 (`ffmpeg -map 0:a -c copy -f hash`) matched across both
  modes and every counted unit on both hosts.

Hosts:

- **rog**: Ryzen 9 9900X in WSL2 with 24 threads, so 24 decrypt slots, and 15 GB. Temp
  and downloads sit on WSL ext4.
- **NAS**: Synology DS923+ with a Ryzen R1600 (2 cores, 4 threads) and 20 GB, in Docker,
  so 4 decrypt slots. Temp sits on an NVMe volume and downloads on the HDD volume, as in
  the test deployment there. Other services ran on the NAS throughout. Fate's time there
  jumped between about 115 s and 150–215 s in both modes while its CPU time stayed at
  140–145 s, so the NAS cannot separate the modes on Fate by less than about 30%.

Medians, low / high, times in seconds and sizes in MiB:

| Host | Scenario | Album | Pool | Runs | Time | Anon | Temp | Written |
| --- | --- | --- | ---: | ---: | ---: | ---: | ---: | ---: |
| rog | matrix | 月姫 | 4 | 4 / 4 | 29.1 / 30.1 | 78 / 634 | 540 / 120 | 1,711 / 869 |
| rog | matrix | 月姫 | 16 | 4 / 4 | 27.9 / 28.7 | 63 / 583 | 383 / 120 | 1,711 / 869 |
| rog | matrix | Fate | 4 | 4 / 4 | 140.7 / 147.8 | 204 / 1,180 | 827 / 72 | 2,586 / 1,414 |
| rog | matrix | Fate | 16 | 4 / 4 | 138.3 / 129.4 | 187 / 1,589 | 836 / 72 | 2,585 / 1,413 |
| rog | 12 MB/s | 月姫 | 16 | 2 / 2 | 76.4 / 76.3 | 89 / 1,538 | 765 / 218 | 1,730 / 888 |
| rog | 12 MB/s | Fate | 16 | 2 / 2 | 105.4 / 106.8 | 187 / 887 | 276 / 72 | 2,656 / 1,486 |
| rog | both albums | both | 16 | 2 / 2 | 102.8 / 153.6 | 188 / 1,890 | 1,061 / 130 | 4,294 / 2,280 |
| rog | temp on Windows volume (9p) | 月姫 | 16 | 3 / 3 | 25.1 / 25.0 | 71 / 874 | 684 / 224 | — |
| rog | temp on Windows volume (9p) | Fate | 16 | 2 / 2 | 113.6 / 103.2 | 177 / 1,748 | 791 / 165 | — |
| NAS | matrix | 月姫 | 4 | 4 / 4 | 42.6 / 42.5 | 168 / 1,264 | 1,105 / 382 | 2,594 / 1,744 |
| NAS | matrix | 月姫 | 16 | 4 / 4 | 46.2 / 46.2 | 112 / 1,497 | 858 / 461 | 2,593 / 1,743 |
| NAS | matrix | Fate | 4 | 4 / 4 | 117.0 / 122.4 | 193 / 506 | 261 / 73 | 3,909 / 2,722 |
| NAS | matrix | Fate | 16 | 4 / 4 | 150.0 / 176.1 | 193 / 881 | 456 / 83 | 3,911 / 2,723 |
| NAS | 12 MB/s | 月姫 | 16 | 1 / 1 | 93.9 / 96.1 | 167 / 1,580 | 996 / 446 | 2,612 / 1,763 |
| NAS | both albums | both | 16 | 1 / 1 | 148.0 / 232.2 | 201 / 2,235 | 1,575 / 472 | 6,508 / 4,466 |
| NAS | temp on the HDD volume | 月姫 | 16 | 2 / 2 | 42.6 / 37.8 | 176 / 1,432 | 1,154 / 484 | 2,590 / 1,740 |
| NAS | temp on the HDD volume | Fate | 16 | 2 / 2 | 185.5 / 146.1 | 187 / 910 | 457 / 81 | 3,900 / 2,714 |

Under a memory limit (NAS, pool 16), low finished every run in its usual time: 月姫 in
45.9 s at 1 GiB and 39.9 s at 512 MiB, Fate in 117.2 s at 1 GiB and 146.5 s at 512 MiB.
High ran out of memory and was killed on 月姫 at 1 GiB (both runs) and on both albums at
512 MiB. It finished Fate at 1 GiB in 112.0 s.

### Reading the numbers

- With decryption in-process, high is no faster than low wherever the network or the CPU is
  the limit. The two modes were within noise on the matrix, at 12 MB/s and with temp on
  rog's Windows volume. With both albums submitted at once, high took about 50% longer on
  both hosts at equal CPU time; the cause was not determined.
- High was ahead only with temp on the NAS's HDD at unthrottled replay speed: 11% on 月姫
  (37.8 s against 42.6 s, two runs each). The Fate difference there is inside that host's
  noise.
- High keeps every downloaded track that has not been decrypted yet as a whole buffer.
  The pipeline allows `max_parallel_downloads` plus `GOMAXPROCS` of those in flight, so its
  peak grows with the CPU count: 1.6 GiB of anonymous memory for Fate on rog, against
  about 0.2 GiB for low on both hosts.
- Low pays with one encrypted checkpoint per track on `temp_dir`. That costs temp peaks of
  0.3–1.6 GiB and, per album, writing the album's encrypted size once more, about
  0.85–1.2 GiB here.

## Post-decrypt disk path

The optimised path is: encrypted input → fragment-by-fragment decrypt → `ffmpeg` stdin →
flattened file → integrity check → tagging → final save. Tagging rewrites only the smaller
`moov` and artwork region for the tail-`moov` layout `ffmpeg` normally produces, leaving
the media in place; compatible layouts that cannot be updated safely in place fall back to
the previous whole-track rewrite.

ROG (Ryzen 9 9900X, WSL Docker) ran an interleaved A/B of the pre-optimisation baseline
against the current implementation, using an offline 192 kHz/24-bit ALAC fixture of about
70.1 MB, 45 segments, roughly 90 seconds. Both ran in the same production image with
`ffmpeg` 6.1.2. Five runs per mode on the same-volume Windows layout after warm-up, three
per mode on the deployment-like layout. Temporary space is the simultaneous peak of
`download.temp_dir` and the system temp directory, excluding the final downloads directory.

| Layout | Mode | Before | Now | Speed-up | Temp before | Temp now |
| --- | ---: | ---: | ---: | ---: | ---: | ---: |
| Temp and output both on the Windows volume | Low | 4774.6 ms | 2585.3 ms | 45.9% | 267.3 MiB | 133.7 MiB |
| Temp and output both on the Windows volume | High | 2962.0 ms | 1341.0 ms | 54.7% | 133.6 MiB | 66.8 MiB |
| WSL-local temp → Windows output volume | Low | 1989.0 ms | 1911.5 ms | 3.9% | 267.3 MiB | 133.7 MiB |
| WSL-local temp → Windows output volume | High | 1947.0 ms | 1845.9 ms | 5.2% | 133.6 MiB | 66.8 MiB |

Every run had external network access blocked and produced no unexpected HTTP requests.
The final files decode completely and their 32-bit PCM SHA-256 matched the golden file on
every run. A tagging micro-benchmark over 64 MiB of media plus a 1 MiB cover went from a
median of 46.0 ms to 11.4 ms (about 4.0×) on the same machine; that gain comes from not
copying the media region and does not translate directly into whole-pipeline speed-up.

Both memory modes have eliminated plaintext intermediate files, so `high` now differs from
`low` by exactly one encrypted checkpoint. On the cross-filesystem layout that resembles a
real deployment, High finished this single-track fixture only about 3.4% faster than Low
while a whole encrypted track raises the heap peak substantially. `low` remains the safer
default. If your temp directory sits on a slow download volume, memory is plentiful and
concurrency is controlled, `high` can still remove that checkpoint I/O. The final
cross-filesystem save still keeps one `.part` copy to preserve complete-file publication
and failure-cleanup semantics; putting `download.temp_dir` on the same filesystem as the
downloads directory degrades that step to an atomic rename.

## End-to-end, by memory mode

To remove Apple CDN variance, the raw encrypted ALAC media for two real albums was cached
first, then replayed read-only with origin fetches forbidden. Everything else is the full
production path: job creation through the API, metadata resolution, `wrapper-manager`
decryption, `ffmpeg` remux, integrity decode, tagging and the final save. Each unit used a
fresh backend container, database, temp directory and output directory, and the clock
started only after `wrapper-manager` had two Ready instances again.

- The formal matrix ran on v1.3.2 / `59befc1`. v1.3.3 / `13c49a7`, merged during testing,
  only refactored the media-discovery code shared by the download paths — source
  resolution, variant selection and transfer logic are equivalent, confirmed with paired
  Low/High smoke runs on the same cache, unit tests and audio verification.
- [月姫 -A piece of blue glass moon- THEME SONG E.P](https://music.apple.com/cn/album/1580904295):
  8 tracks, about 841 MiB of raw media.
  [Fate/stay night [Realta Nua] Soundtrack Reproduction](https://music.apple.com/cn/album/1576634760):
  62 tracks, about 1.14 GiB.
- `download.max_parallel_downloads` and `download.max_parallel_decrypts` were both set to
  1, 2, 4 and 8. Each album × mode × D/X combination ran three interleaved rounds; the
  tables are three-round averages.
- D/X are process-wide pools shared across jobs, not "at most p tracks per job". The
  internal in-flight ceiling is D+X, so at D=X=p the pipeline allows up to 2p downloaded
  but not yet decrypted tracks.
- Memory is the whole container's cgroup peak, including process memory and the file page
  cache charged to the cgroup. Temporary space is the simultaneous peak of
  `download.temp_dir` and the system temp directory, excluding the downloads directory.
- All 48 units completed, ALAC was forced throughout, and there were zero failed tracks and
  zero cache origin fetches. Per-track audio packet SHA-256 matched across both modes at
  every concurrency.

**月姫** — long Hi-Res tracks, which amplify whole-track memory and checkpoint costs:

| D/X | Low time | High time | High gain | Low cgroup peak | High cgroup peak | Low temp peak | High temp peak |
| ---: | ---: | ---: | ---: | ---: | ---: | ---: | ---: |
| 1/1 | 51.3 s | 44.6 s | 13.0% | 137 MiB | 772 MiB | 394 MiB | 135 MiB |
| 2/2 | 32.1 s | 26.7 s | 16.9% | 150 MiB | 1,345 MiB | 714 MiB | 233 MiB |
| 4/4 | 23.8 s | 19.3 s | 19.2% | 227 MiB | 1,931 MiB | 1,260 MiB | 446 MiB |
| 8/8 | 22.1 s | 18.8 s | 14.8% | 344 MiB | 2,017 MiB | 1,550 MiB | 677 MiB |

**Fate** — many short tracks, so fixed session, `ffmpeg` startup, verification and tagging
overheads weigh more:

| D/X | Low time | High time | High gain | Low cgroup peak | High cgroup peak | Low temp peak | High temp peak |
| ---: | ---: | ---: | ---: | ---: | ---: | ---: | ---: |
| 1/1 | 157.1 s | 134.3 s | 14.5% | 210 MiB | 369 MiB | 144 MiB | 72 MiB |
| 2/2 | 75.6 s | 69.4 s | 8.2% | 218 MiB | 485 MiB | 217 MiB | 72 MiB |
| 4/4 | 63.1 s | 58.7 s | 6.9% | 214 MiB | 641 MiB | 245 MiB | 98 MiB |
| 8/8 | 59.1 s | 56.0 s | 5.3% | 266 MiB | 960 MiB | 382 MiB | 106 MiB |

Fate 1/1 High was 160.2 s on the first round and 121.6 s / 121.0 s on the next two — a
wall-clock coefficient of variation of 16.7%; an extra run measured 119.9 s. The table
keeps the pre-agreed three-round average without discarding the cold sample. Every other
combination had a three-round CV between 0.2% and 8.5%.

### Reading the numbers

High does not use a faster decryption algorithm. Both modes decrypt fragment by fragment
straight into `ffmpeg`. The difference is that Low writes a resumable encrypted `raw-*`
checkpoint to the temp volume and reads it back, while High substitutes an in-memory whole
track for that write and re-read. That is why High is roughly 5–19% faster here with about
5–9% less container CPU time — the saving is filesystem I/O and syscalls, which
Windows/WSL mounted volumes amplify. On a slow public download or with the temp directory
on fast local storage, that I/O is hidden behind network or CPU work and the gap usually
narrows.

For a track being remuxed, Low's temp directory holds similarly sized `raw-*` and `flat-*`
files at once, so about twice a single track's size is expected — it is not a plaintext
`dec-*` reappearing. Across a concurrent album the peak also depends on which tracks happen
to be in which stage, so it is not strictly double. High mostly keeps only `flat-*`.

High's memory cost cannot be estimated per worker or per average album track: every
downloaded-but-not-yet-decrypted track keeps a complete encrypted buffer until decryption
consumes it, on top of Go's GC allocation headroom. At D=X=p the internal ceiling allows
D+X media in flight, so the peak grows quickly at high concurrency. Keep the default `low`
when the memory budget is unclear; with `high`, start at 1/1 or 2/2 and raise it only after
confirming that temp-disk I/O is the bottleneck and that the peak fits. Local replay shows
the backend's processing ceiling, not absolute download times on an ordinary CDN.
