# internal/applemusic

Apple Music link parsing and catalog reads. The host split, token caches and the
request path are in the package comment (`go doc ./internal/applemusic`); root
`AGENTS.md` "Two Apple hosts" says why it matters.

- A new amp-api read follows `MotionArtworkViaWebToken`: a failure, timeout or
  unexpected shape means "not available" to the caller, with a test for that path.
- Before adding a field or `extend=`, read Apple's documentation; developer.apple.com
  pages come as Markdown when `.md` is appended to the URL
  (https://developer.apple.com/documentation/applemusicapi.md). A field that exists
  only on amp-api has no documentation, so the PR carries a captured response as
  evidence.
- Strip `Authorization`, `Media-User-Token`, cookies, the developer token and the
  scraped JWT from any captured request or response before it becomes a fixture or
  a log line.

## Verify

```bash
go test ./internal/applemusic/
```

Live behavior needs network access to Apple: `make dev` and a submitted album, or a
`POST /api/v1/quality` probe, exercise both hosts without a wrapper.
