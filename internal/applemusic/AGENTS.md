# internal/applemusic

Parses Apple Music links and reads catalog, artwork and library metadata through
`CatalogClient`. The package comment (`go doc ./internal/applemusic`) lists the
entry points and token-cache invariants.

## Two Apple hosts, not one

`api.music.apple.com` is the documented API and takes the self-signed developer
token. `amp-api.music.apple.com` is Apple's internal web-player endpoint and only
answers to a JWT scraped out of music.apple.com's JS bundle, with an `Origin`
header attached.

Some fields exist **only** on the amp-api side, and no amount of `extend=` will
coax them out of the public host: `enhancedHls` and `editorialVideo` (animated
covers) are both like this. Apple states outright that a third party may load
only `artistUrl` as an extended attribute on Albums. So MusicKit in the iOS app
cannot reach them either; it goes through the public host. Verified on a real
device, not assumed.

`CatalogClient.apiBase` picks the host, and `CatalogClient.doWithWebAuth` handles
the scraped token. When a field turns up empty for no apparent reason, check which
host you asked.

Being undocumented, amp-api can change without notice. Anything read from it must
degrade to "this album doesn't have that" rather than failing a job.

## Invariants

- An amp-api read that fails, times out or returns an unexpected shape means "not
  available" to the caller, never a job error. `MotionArtworkViaWebToken` is the
  model; a new amp-api field follows it and gets a test for the failure path.
- Every Apple request goes through `CatalogClient.doAppleRequest`, the client's one
  request gate and its HTTP client with a timeout; response bodies are read
  through a size cap. Build new requests on those helpers, not a bare
  `http.Client`, so rate limits and bounds stay in one place.
- The catalog token and the web-player JWT are two separate `tokenCache` values;
  `CatalogClient.doWithAuth` is the one authorized-request path, and a 401 drops
  only the rejected token and retries once. Add a request through
  `doWithCatalogAuth` or `doWithWebAuth`, never with its own token handling.
- A non-2xx JSON answer goes through `decodeCatalogResponse` and becomes a
  `catalogRequestError`: `NonRetryable` for a 4xx, `RetryDelay` from
  `Retry-After`. Its `Error()` is the short message job rows and API responses
  show and never quotes the response body; the raw excerpt stays on the value
  for log lines via `UpstreamBody` (`logging.UpstreamBody` finds it through
  wrapping). Other deterministic failures wrap a `nonRetryableError`
  sentinel such as `ErrNoEnhancedHLS`, so retry helpers in internal/media stop
  at once instead of backing off.
- `EnrichWithAlbum` is the only place album-level fields are merged into a song;
  callers in other packages use it rather than a copy.
- Never log or persist a token, the scraped JWT or a media-user-token; strip
  `Authorization`, `Media-User-Token` and cookies from any captured request before
  saving it as a fixture.

## Before adding a field or `extend=`

1. Read Apple's documentation first. developer.apple.com pages can be fetched as
   Markdown by appending `.md` to the URL, for example
   https://developer.apple.com/documentation/applemusicapi.md.
2. Decide which host the field lives on. If it is amp-api only, say so in the code
   comment and in the PR, with the evidence (a captured response, minus tokens),
   because there is no documentation to point at.
3. Map it in one place and add a fixture-based test with only the fields under test.

## Verify

```bash
go test ./internal/applemusic/
```

Live behavior needs network access to Apple: `make dev` and a submitted album or a
`POST /api/v1/quality` probe exercise both hosts without wrapper-manager.
