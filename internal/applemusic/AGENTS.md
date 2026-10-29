# internal/applemusic

Apple Music link parsing and catalog reads, all on amp-api. The token cache and the
request path are in the package comment (`go doc ./internal/applemusic`); root
`AGENTS.md` "Apple catalog reads" lists which reads are required and which degrade.

- A required read (the link's resource, a track's song metadata) returns its error,
  a `catalogRequestError` for a non-2xx answer, so the caller's retries decide.
- A new optional read follows `MotionArtworkViaWebToken` or `libraryArtworkURL`: a
  failure, timeout or unexpected shape means "not available" to the caller, with a
  test for that path. `Song` treats its album enrichment the same way.
- Before adding a field or `extend=`, read Apple's documentation; developer.apple.com
  pages come as Markdown when `.md` is appended to the URL
  (https://developer.apple.com/documentation/applemusicapi.md). A field that exists
  only on amp-api has no documentation, so the PR carries a captured response as
  evidence.
- Strip `Authorization`, `Media-User-Token`, cookies and the scraped JWT from any
  captured request or response before it becomes a fixture or a log line.

## Verify

```bash
go test ./internal/applemusic/
```

Live behavior needs network access to Apple: `make dev` and a submitted album, or a
`POST /api/v1/quality` probe, exercise these reads.
