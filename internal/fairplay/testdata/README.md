# FairPlay golden vector

`track_1720704575_s1.{json,ct,pt}` is Temari's public test track 1720704575_s1:
the key template (`.json`), one encrypted sample (`.ct`) and its plaintext
(`.pt`), decoded from the embedded copies in Temari's
`bindings/go/temari_test.go` at v0.5.1 (MIT License; see `../doc.go`).

**Shared fixture, do not move or rename.** Besides `fairplay_test.go`, these
files are read by relative path from:

- `internal/wrapper/decrypt_test.go` (decrypt through the wrapper's template
  path: `../fairplay/testdata/`)
- `tools/temaridiff` (a separate Go module that diff-tests this package against
  upstream Temari: `diff_test.go` and `bench_test.go`, via
  `../../internal/fairplay/testdata/`)

Changing a file here changes what all three check; update them together.
