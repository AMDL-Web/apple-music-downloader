# Command line

> Type: reference

`amdl` (`apps/amdl`) is a client of the API in [api.md](api.md), written for agents and
scripts first: every result is JSON on stdout, every error is the API's error object on
stderr, and the exit status says what kind of failure it was. Its commands come from one
table, `internal/capability#All`, which the MCP server shares; the parameters of a command
that passes a request through are read from `internal/api/openapi.yaml`, so `--help` and
`amdl schema` always match the server's contract.

## Finding a command

| Command | Answers |
| --- | --- |
| `amdl` | With stdout not a terminal, a short JSON list of every command; in a terminal, the same as text, after a two-line quick start |
| `amdl cli search "<task>"` | The five commands that best match a task, offline: `[{command, summary, example}]` |
| `amdl cli commands` | Every command with its MCP tool and safety level |
| `amdl schema <command>` | The command's flags, positional arguments, safety, exit statuses, examples, and each API operation it calls: method, path and parameters. `--view full` adds the parameters' descriptions, the request body and the output JSON Schema |
| `amdl <command> --help` | The same as text |
| `amdl <group>`, `amdl <group> --help` | The commands under a group such as `jobs` or `library sync`: `{group, commands: [{command, summary}]}`, text in a terminal |
| `amdl --version`, `-v`, `-V` | The same as `amdl version` |

An unknown flag fails with `usage_error`, `details.flags` (the command's own flags) and
`details.did_you_mean` (those spelled almost the same), and a hint to `--help`.
An unknown command fails with `usage_error` and `details.did_you_mean`: up to three
commands spelled almost the same (`jobs lst` suggests `jobs list`), then the best matches
by meaning.

## Commands

| Command | API | Safety |
| --- | --- | --- |
| `search <term> [--types songs,albums,playlists,artists] [--storefront] [--limit] [--offset]` | `GET /search` | read |
| `artists albums <artist_id> [--storefront] [--limit] [--offset]` | `GET /artists/{artist_id}/albums` | read |
| `albums get <album_id> [--storefront]` | `GET /albums/{album_id}` | read |
| `lookup <url>` | `POST /lookup` | read |
| `quality <url>` | `POST /quality` | read |
| `download <url>... [--codec] [--codec-fallback] [--max-sample-rate] [--max-bit-depth] [--wait\|--watch] [--timeout] [--progress-interval]` | `POST /downloads` | write |
| `jobs list [--status] [--type] [--q] [--sort] [--order] [--limit] [--offset]` | `GET /downloads` | read |
| `jobs get <job_id> [--view summary\|full]` | `GET /downloads/{job_id}` | read |
| `jobs watch [<job_id>] [--types] [--progress-interval] [--timeout]` | the job's event stream, or the overview feed | read |
| `jobs wait <job_id> [--timeout]` | the job's event stream | read |
| `jobs cancel <job_id>`, `jobs retry <job_id>`, `jobs move <job_id>` | `POST …/cancel`, `…/retry`, `…/move` | write |
| `jobs delete <job_id>` | `DELETE /downloads/{job_id}?record=true` (a record's run deletes its record, as in the apps) | destructive |
| `library list [--state] [--flag] [--source] [--q] [--sort] [--limit] [--offset]`, `library playlists [same]` | `GET /library/albums`, `GET /library/playlists` | read |
| `library get <id>`, `library playlist <id>`, `library summary` | `GET /library/albums/{id}`, `GET /library/playlists/{id}`, `GET /library/summary` | read |
| `library add --urls [--codecs] [--now] [--include all\|albums]` | `POST /library/add` | write |
| `library download <kind> <id> [--codec] [--songs] [--now]`, `library pause <kind> <id>`, `library resume <kind> <id>` | `POST /library/{kind}/{id}/download`, `…/pause`, `…/resume` | write |
| `library reset <kind> <id> --what [--slot] [--songs] [--codecs] [--states]`, `library reset-all --what --filter <json> [--kind]` | `POST /library/{kind}/{id}/reset`, `POST /library/reset` | destructive |
| `library delete <kind> <id>` | `DELETE /library/{kind}/{id}` | destructive |
| `library check [--kind --id] [--force] [--wait] [--timeout]`, `library check-status` | `POST`/`GET /library/check` | write, read |
| `library pending list [--state]`, `library pending retry <pending_id>`, `library pending delete <pending_id>` | `/library/pending`, `…/{pending_id}/retry`, `DELETE …/{pending_id}` | read, write, destructive |
| `library sync get`, `library sync reset [--account]` | `/library-sync`, `…/reset` | read, destructive |
| `apple-library list <kind> [--account] [--sort] [--order] [--q] [--limit] [--offset]`, `apple-library tracks <playlist_id> [--account]`, `apple-library status` | `GET /apple-library/{kind}`, `…/playlists/{playlist_id}/tracks`, `…/status` | read |
| `apple-library sync [--account] [--mode full\|incremental]` | `POST /apple-library/sync` | write |
| `accounts` | `GET /accounts` | read |
| `accounts set <account_id> [--library-sync on\|off] [--apple-library on\|off] [--default\|--position <n>]` | `PATCH /accounts/{account_id}` | write |
| `accounts delete <account_id>` | `DELETE /accounts/{account_id}` | destructive |
| `logs list [filters]`, `logs tail [filters]` | `GET /logs`, `/logs/stream` | read |
| `system get`, `system health`, `system stats [--days]`, `hooks list` | `/system`, `/health`, `/stats`, `/hooks` | read |
| `system restart` | `POST /system/restart` | write |
| `hooks presets`, `hooks switch --enabled`, `hooks set <name> [--preset <id> --values <json>] [--url] [--events] [--headers <json>] [--body] ...`, `hooks test <name>`, `hooks delete <name>` | `GET /hooks/presets`, `PUT /hooks`, `PUT`/`DELETE /hooks/{name}`, `POST …/test` | read, write, write, write, destructive |
| `config get [key...]`, `config set <key=value>...`, `config reload` | `GET`/`PUT /config`, `…/reload` | read, write, write |
| `wrapper status`, `wrapper login [--username <id>] [--password-stdin]`, `wrapper 2fa <login_id>`, `wrapper logout --username <id>\|--account-id <id>`, `wrapper keygen` | `/wrapper/*`, local | read, write, write, destructive, read |
| `context list\|add <name> <url>\|use <name>\|remove <name>`, `version` | local, `/system` | |
| `serve [--config] [--hooks] [--init key=value]... [--no-console]` | runs the backend in this process | |

`accounts` lists the Apple Music accounts the wrapper holds, the default first: `id`,
`name` (the Apple ID), `region`, `default`, `state` (`active`, or `missing` once the wrapper
no longer reports it) and the two switches `library_sync` and `apple_library`; add
`--fields id,name,region,default,state,library_sync,apple_library` to keep just those.
Wherever a command takes an account (`accounts set` and `delete`, `--account` on
`apple-library list`, `tracks`, `sync` and `library sync reset`, `wrapper logout
--account-id`), it may be given as the account's id, its Apple ID, or its region
(`jp`, any case). A region names the single active account serving it; with two it is
refused as `invalid_request` and the message lists their ids, and with none as
`account_not_found` (exit 3). Without `--account`, the apple-library commands and
`library sync reset` act on the default account. `accounts set` takes at least one
switch or `--default`; `--default` is `--position 0`. `accounts delete` logs the account
out of the wrapper and drops its switches, library sync anchor, Apple library cache and
stored token, while downloaded files and records stay; `wrapper logout` keeps the row
and its switches for when the account logs in again. Adding an account is `wrapper
login`, which fails with `account_limit_reached` (exit 4) once the wrapper-manager holds
ten.

`download -` reads links from stdin, any number per line. `config set` checks its keys
against the runtime config first and refuses an unknown one as `config_key_not_found`,
with the closest key in `details.did_you_mean`. It takes dotted keys
with YAML values (`download.quality_priority=[alac,aac]`) and answers with those keys'
new values, `persisted` and `pending_restart` (the keys saved for the next start, which
`amdl system restart` applies). A password or two-step code
never goes on the command line. In a terminal, `wrapper login` asks for the Apple ID
when `--username` is missing, then for the password and, when Apple sends one, the
two-step code, neither echoed. Elsewhere it needs `--username` and `--password-stdin`
and reads the password from the first line of stdin; a `needs_2fa` answer then carries
the `login_id` for `wrapper 2fa <login_id>`, which reads the code the same way (a
prompt in a terminal, else stdin's first line) and has to follow within 60 seconds.

`wrapper keygen` prints `{"key": …}`, a strong random key for `wrapper.token` (32 random
bytes, base64url), and contacts no server: set it on the wrapper's gateway and with
`amdl config set wrapper.token=<key>` ([configuration.md](configuration.md#wrapper)).

`--storefront` (the `storefront` argument of `search_catalog`, `list_artist_albums` and `get_album`)
reads the catalog of another region: pass one a logged-in account serves (`accounts` shows each
account's `region`), such as `jp`; every hit's `url` carries it. Without it, the default account's region answers.

What is downloaded is what amdl's records say, never the disk: `library list` and
`library get` show them, and `search`, `lookup` and `albums get` mark what they find with
its record's `resource` (`albums get` gives each song in its `tracks` its `state`). `download` of a record
with every track done answers `already_downloaded` and queues nothing, ignoring `--codec`; the answer then
carries a `hint` naming `library download <kind> <id> --codec <codec>`, which adds that quality
as a second file. `library reset`
downloads it again (`--what audio`), and `library check` then `library reset --what audio
--states missing` brings back files deleted outside amdl. `{kind}` is `albums` or
`playlists`. `jobs list` is the history: one job per download.

## Output

- **stdout holds one JSON value**, the API's response body as the spec describes it,
  indented and coloured in a terminal and on one line in a pipe. A change with an empty
  answer prints nothing. `NO_COLOR` turns colour off. Times are RFC 3339 in UTC, as the
  API gives them; the backend's log lines use local time with its offset.
- **`--fields a,b`** keeps only those keys: of the object itself when it has one of
  them, otherwise of each element of the arrays inside it (the jobs of a list, the hits of
  each search group). `search`, `lookup`, `artists albums` and `albums get` leave out the
  artwork color palette (`artwork_bg_color`, `artwork_text_color1` to `4`) unless
  `--artwork-colors` is given.
  Every command takes it except `mcp`.
- **`jobs get` prints a summary by default**: the job, `album_progress`
  (`total` tracks; `finished`, every track that ended, cancelled and failed ones included;
  `done`, those with a file, saved or already on disk; `failed`; and the overall `percent`,
  weighted by stage) and one short entry per track
  (`index, title, status, quality, file_size, output_path, error`). `--view full` prints
  the API's `DownloadDetail`.
- **Streams are NDJSON.** `jobs watch <job_id>` prints each `Event` as one line,
  `item_progress` at most every `--progress-interval` per track, and ends with
  `{"type":"job_snapshot","job":…}` once the job has finished. Without an id it prints the
  overview feed's messages until interrupted. `logs tail` prints `LogEntry` lines.
- **stderr** carries notes for people only when it is a terminal: the server in use
  (`-> nas (http://…)`) and a progress line while waiting. `--quiet` drops them. Anywhere
  else stderr holds nothing but the error object.

## Waiting

- `download --wait` and `jobs wait` return as soon as the job is finished, without waiting for
  its post-download hooks. `download --wait` follows the links that have a job: a link
  answered `pending` (a song, an artist or a library playlist, read into records first) or
  `already_downloaded` has none, and when no link has one it prints the submit answer. A finished job's summary, here and in `jobs get`, carries
  `hooks_running: true` while any still run.
- `download --watch` and `jobs watch <job_id>` keep streaming until the hooks have ended too.
- A finished job never ends in `wait_timeout`, and `--timeout 0` (or a negative value) looks
  once: a finished job gives its normal result and exit status, a running one `wait_timeout`.
- `--timeout` takes a duration such as `90s`, `20m` or `2h`; it defaults to `2h` for
  `jobs wait` and for `download --wait|--watch` (all its jobs together), and to no limit for
  `jobs watch` and `logs tail`, which otherwise run until interrupted. When it runs out,
  `jobs watch` without an id and `logs tail` end normally.
- A timed-out wait prints the job it was waiting for before the error: an object for one job,
  an array for several.
- A job that ends without completing exits 7 with `job_failed`, and the `hint` says what to
  do: `amdl library resume <kind> <id>` for a run that ended cancelled with the error `paused`
  (its record was paused; resuming queues a new run, whose id `jobs list --q <id>` or the
  record's `active_job_id` gives), a note that a retry fails again when the error says the
  catalog does not have the album or playlist in its storefront (with
  `amdl lookup <link>` and `amdl library delete <kind> <id>`), and `amdl jobs retry <job_id>`
  otherwise.
- `library check` returns at once with the check just started. `--wait` follows it, asking
  every second, until it is finished and prints `{check}` (`--timeout`, default `10m`; `0`
  looks once; `wait_timeout` when it runs out). A check that ended `too_many_missing`
  carries a `hint` with the command that repeats it with `--force`.

## Errors and exit statuses

A failure writes `{"error":{"code","message","details"?,"hint"?}}` to stderr: the API's
error object as sent, or one of the CLI's own codes below, with `hint` naming the command
to run next.

| Status | Meaning | Codes |
| --- | --- | --- |
| 0 | Done, including a change that was already in place | |
| 1 | Internal error | `internal_error`, `store_error` |
| 2 | Input rejected; change it rather than retry | every 400, 413 and 422 code, `usage_error` |
| 3 | Not found | the 404 codes, `context_not_found`, `config_key_not_found` |
| 4 | State conflict | the 409 codes |
| 5 | Unavailable; retry later | 502, 503 and 504 codes, `server_unreachable`, `server_not_configured` |
| 6 | Wrapper sign-in failed, or its gateway refused the key | `authentication_failed`, `login_unsupported`, `wrapper_unauthorized` |
| 7 | A waited-for job failed or was cancelled | `job_failed` |
| 8 | The wait ended before the job did | `wait_timeout` |
| 10 | Confirmation needed; nothing was changed | `confirmation_required` |
| 130 | Interrupted | |

A download whose links are all already queued or running is not an error: it exits 0
and prints the batch, whose results carry `existing_job_id`.

## Safety

- **Destructive commands** (`jobs delete`, `library reset`, `library reset-all`,
  `library delete`, `library pending delete`, `library sync reset`, `accounts delete`, `wrapper logout`,
  `wrapper uninstall --purge`, and `wrapper install` when it
  would install QEMU) ask `[y/N]` when stdin and stderr are a terminal and otherwise
  need `--yes`; without it they exit 10 before changing anything.
- **`--dry-run`** on every command that changes something, except `mcp`, prints
  `{"method","url","body"}` and sends nothing; it needs no reachable server.
  `service install` and `service uninstall` take it too and print their plan
  (`{"dry_run":true,"action","manager","definition",…}`, with the command line, log and
  config paths for `install`, its console URL with `<port>` for the free port the real
  install picks and keeps, and a `note` that also says when a backend already runs on
  the local files), writing and registering nothing.

## Which server

The first of these that is set wins:

1. `--server`, a URL, a saved context name, or `embedded`. A single word with no `:` or `.`
   that is no saved context is `context_not_found`, not a host name; give `host:port` or a URL.
2. `AMDL_URL`, the same.
3. The current context, saved by `amdl context add` or `amdl context use` in
   `cli.yaml` under the user config directory (`~/Library/Application Support/amdl` on
   macOS, `$XDG_CONFIG_HOME/amdl` or `~/.config/amdl` on Linux). A current context on a
   loopback address that does not answer (a closed tunnel, an old port) gives way to the
   next item when that backend runs, with a note on stderr; one on another host is kept.
4. The backend running on this machine for the desktop app, `amdl service` or
   `--server embedded`, when its runtime file names one that answers ([Running the backend in amdl](#running-the-backend-in-amdl)).
5. `http://127.0.0.1:18080`, when its health check answers at once (`apps/amdl/internal/cli#probe`).

Otherwise the command fails with `server_not_configured`, except `amdl mcp`, which
starts anyway on `http://127.0.0.1:18080`.

`amdl context add` saves the server, and makes it current when it answers a health check.
One that does not answer is saved but not made current, so a server can be registered
before it starts; the result is `{name, url, current, reachable}` plus a `hint` to run
`amdl context use <name>` once it answers.

## Serving

`amdl serve` runs the backend and web console in the process, as
`internal/server#Run`: `--config` (default `$AMDL_CONFIG`, else
`configs/config.yaml`) and `--hooks` (default `$AMDL_HOOKS_CONFIG`, else
`configs/hooks.yaml`), stopping on SIGINT or SIGTERM and ignoring SIGHUP. It listens
on `server.listen` from the config; a missing config is created from the built-in example,
with each repeatable `--init key=value` written into it
([configuration.md](configuration.md#environment-variables-and-first-values)). No
environment variable sets a key. `POST /api/v1/system/restart` (`amdl system restart`)
stops it and starts it again in the same process. Trying it in simulate mode with no wrapper:
[install.md](install.md#try-it-without-a-wrapper). `amdl serve --local` uses this machine's local files instead
([Running the backend in amdl](#running-the-backend-in-amdl)), on a free loopback port
or `--listen`; with `--background` it logs to `logs/server.log` beside them and, on
Windows, closes a console window it was given alone. Either way the `amdl backend
listening` log line carries `addr` and, unless `--no-console`, `console`, the URL to open
(a wildcard address shows as `127.0.0.1`).

## MCP server

`amdl mcp` serves the same capabilities to an MCP client over stdio, talking to the
server picked as above: `claude mcp add amdl -- amdl mcp --server nas`. It answers the MCP
handshake even when no server is configured or answering, so the client lists the tools;
each call then fails with `server_unreachable`, naming the address and with a `hint` to
start the backend or pass `--server`, until a server answers at
`http://127.0.0.1:18080` or the `--server` the client passed. It takes neither `--dry-run`
nor `--fields`. Each capability
of `internal/capability#All` that names a tool becomes one, with the safety level as its
annotations (`readOnlyHint`, `destructiveHint`, `idempotentHint`, `openWorldHint` for the
tools that read Apple's catalog; `set_config` and `cancel_job` count as destructive too,
`apps/amdl/internal/mcpserver#alsoDestructive`) and, for a tool that passes a request through, the
operation's parameters and body fields as its input schema. Every parameter is described,
and an argument the tool does not declare or of the wrong JSON type is refused, naming the
field. A result is the JSON in one text block, with no structured copy of it. A failure is
a tool error whose text is the error object of the [Errors](#errors-and-exit-statuses)
table, with the same codes as the CLI: `invalid_request` for a bad argument,
`config_key_not_found` for an unknown key of `get_config`, and `no_urls_accepted` when
`start_download` queues nothing (`details.rejected` says why per link), except that links
which are all queued or running already are not an error. `set_config`, like
`config set`, answers with the keys it set, `persisted` and `pending_restart`.

- **From a search to a download takes two calls**: `search_catalog`, then
  `start_download` with the hits' `url` values. It answers at once with
  `accepted[]` (`{url, status, job_id?, resource?}`: `accepted` with the record's run,
  `pending` for a link read into records first, `already_downloaded` with nothing queued)
  and `rejected[]`; links already queued or running come back rejected with
  `existing_job_id`. `reset_resource` downloads a record again, `check_library` checks its
  files on disk (read the result with `get_library_check`), and `list_library`, `get_resource`
  (an album), `list_library_playlists`, `get_library_playlist` and `get_library_summary` read the
  records. `download_resource` downloads a record now or adds a second quality with `codec`
  (`{status: queued, job}`, whose `job.id` `wait_for_job` follows), `pause_resource` and
  `resume_resource` stop and continue a record, and `delete_resource` removes it. A link
  `start_download` answers `already_downloaded` for a `codec_priority` carries a `hint` naming
  `download_resource`. This shape is the MCP's own: the CLI's `download`
  prints the API's batch, whose jobs are `results[].job.id`. `search_catalog`,
  `list_artist_albums`, `get_album` and `lookup_link` leave out the artwork color palette unless
  `artwork_colors` is true.
- **`wait_for_job`** follows one job for at most `timeout_s` seconds
  (`apps/amdl/internal/mcpserver#maxWait`, default 120; a value outside 1 to 300 is an
  `invalid_request` error) and answers `{done, job}`, the job in the summary view of
  `jobs get`, as soon as the job is finished, with `hooks_running: true` while its hooks
  still run. While it waits it sends `notifications/progress` with the
  album's percent, when the call carried a progress token. On `done: false`, or when the
  client lost the request, calling it again is safe.
- **`list_jobs`** returns compact rows (`apps/amdl/internal/jobview#Row`), 50 by default as
  in the CLI, each with the `resource` its run downloads for, and
  **`get_status`** gathers the system, health, wrapper, accounts and library-sync answers.
- **Accounts**: `list_accounts` lists them, `update_account` switches an account's library
  sync or Apple library or moves it (`position` 0 makes it the default) and `delete_account`
  drops it. `list_apple_library`, `list_apple_library_playlist_tracks` and
  `sync_apple_library` take an `account`; `update_account` and `delete_account` take
  `account_id`. Each is an id, an Apple ID or a region, resolved as in the CLI, and the
  Apple library tools use the default account without one.
- **No tool signs the wrapper in**: a password never passes through a model. When
  `get_status` shows no account, a person runs `amdl wrapper login` or uses the console.

## Terminal interface

`amdl` with no arguments in a terminal, or `amdl tui`, opens an interactive interface
for people (`apps/amdl/internal/tui`). It renders inline rather than taking over the
screen: what is done goes into the terminal's own scrollback (the banner, each request
and its answer, and one line per job when it finishes), and only the bottom of the
screen is redrawn. A finished line counts the tracks saved, then `✗` and the number that
failed, if any; under a job that failed it names `/retry` with the job's number, which
starts a new job for the tracks that did not finish (`POST /downloads/{id}/retry`).
`/library [text]` lists the album records, each with how much is downloaded and what holds
it up.

- **The banner** opens the session: the logo, amdl and its version, the server with its
  wrapper's state, the server's download folder with the space left there, and how many
  jobs the server has saved and failed so far.
- **The prompt** takes a pasted Apple Music link (several at once download as a batch),
  which shows a preview to confirm with Enter; plain text followed by Enter, which
  searches the catalog (nothing is searched while typing) and lists the hits (↑↓ to
  move, Space to select several, Tab to a type's tab, which reads its group a page at a
  time (`apps/amdl/internal/tui#pageSize`) and the next as the cursor nears the end, →
  to open an artist's albums, Enter to download); or a `/` command, listed as you type
  it (↑↓ choose, Tab completes, Enter runs; `/help` shows them all, `/login` signs the wrapper in without
  echoing the password). Esc closes a panel, drops a search still in flight, or clears
  the prompt.
- **`/quality`** lists the qualities of `download.quality_priority`, the ones on in the
  order the server tries them, then the rest: shift+↑↓ moves one a place, shift+Home and
  shift+End to the first or last place, Space turns one on or off (one stays on; AAC-LC
  is always tried last and not listed), Enter saves.
- **`/queue`** lists the queued jobs in the order the server starts them, with the same
  keys: shift+↑↓ moves one a place, shift+Home and shift+End to the front or the back.
  Each move goes to the server at once (`POST /downloads/{id}/move`), one at a time, and
  the queue is read back after the last; when the server answers `job_not_queued`, a job
  having started or ended meanwhile, the panel shows the server's order again.
- **A download from search asks first** only where there is a choice, below the prompt:
  Lossless or Dolby Atmos where a hit has both and the server's
  `download.quality_priority` lists both (`internal/format#QualityChoices`; the one it
  lists first is chosen), and, for what is already in the library, whether to keep it or
  download it again. The quality goes as the add's overrides; downloading again resets
  the hit's record (`POST /library/{kind}/{id}/reset`, `what: audio`, the song alone for
  a song hit). A pasted link downloads with the server's settings.
- **Above the prompt** sits one card per job in progress, up to three, in the order the
  server runs them: the running jobs in the order they started, then the queue in its
  order, so the job that starts next keeps its place. A card holds its title and session
  number, the album bar of
  `internal/progress#AlbumProgress` (finished tracks in the order they finished, the
  rest of the progress dashed, one cell per track up to `internal/progress#TickMax`)
  with the count, percent, time left and average speed (`internal/progress#AvgSpeed`),
  and three track rows with a seven-stage bar
  each. A queued job's card, and one still reading its tracklist, is one line; a running
  card keeps its height until the job ends (`apps/amdl/internal/tui#tracksPerCard`). The card takes its color from the album's
  artwork. `ctrl+o` shows every track, `ctrl+t` every job.
- **Jobs have session numbers**, `#1`, `#2`, on their cards, finished lines and in
  `/jobs` and `/library`, which `/cancel`, `/retry`, `/remove`, `/copy` and `/logs` take,
  as well as the start of a job ID.
- **Below the prompt** open the panels (the `/` menu, search hits, the preview, help),
  so opening one does not move the prompt, and under everything one status line: the
  connection, the job counts, and the keys of the moment that a person would not guess
  (Esc and the arrows in a list go unsaid; `/help` lists every key).
- **Widths**: past 120 columns the layout, and what goes into the scrollback, stays 120
  wide, wrapped by amdl rather than the terminal; narrow terminals drop the
  per-track status column, then the artist and the track rows, and below 40 columns
  the prompt's border; short ones keep fewer cards (`apps/amdl/internal/tui#wideCols`
  and its siblings). When the width changes, the screen and its scrollback are cleared
  and the session's output is printed again at the new width, since a terminal that
  rewraps its lines would leave copies of the old bottom region on screen. `ctrl+l`
  does the same on demand; what the terminal showed before amdl started is cleared too.
- **Colors** follow the terminal's background (light or dark) and color depth;
  `NO_COLOR` with any value turns them off, and the bars and states still read by their
  shapes (━ done, ╍ in progress, ─ not started, ✓ ✗ ⊘).
- **Leaving** takes `ctrl+c` twice, or `ctrl+d` on an empty prompt. Downloads keep
  running on the server.

`make tui-web` builds the same interface for browsers (`apps/amdl/web`): it runs in
xterm.js over the made-up backend of `internal/demo`, which the desktop app's demo runs
on too.

## Running the backend in amdl

`--server embedded` runs a command against a backend inside the amdl process, the way
the desktop app's standalone mode does, with the same files (`internal/local#Dirs`):
the config, database and runtime file under `AMDL/server` in the user config directory,
scratch files in the user cache directory, downloads in `~/Music/AMDL` unless the config
says otherwise. When a backend is already running there, such as the desktop app's, the
command connects to it instead, and so does a command that names no server at all.
Otherwise amdl starts one on a loopback port and stops it when the command ends;
unfinished jobs resume on its next start, and a `download` waits for its jobs so they
can finish. A `pending` link has no job to wait for; the backend reads it on its next start. The first start needs the wrapper's address, from `--wrapper`, and fails with
`wrapper_not_configured` without it. Later starts keep the address in the config, which
`amdl config set wrapper.address=...` and `amdl system restart` change; `--wrapper`
writes a new one.

Whichever process runs the backend holds `backend.lock` beside the database, and the
OS releases it when that process exits. A second `--server embedded` finds it held and
waits up to `apps/amdl/internal/cli#busyWait` for the other backend to answer; `amdl
serve --local` waits for as long as it takes, then takes over.

## Background service

`amdl service install` registers `amdl serve --local --background` to start at login,
so local downloads go on with no window or terminal open, and starts it:

| OS | Registered as | Restarted after a crash |
| --- | --- | --- |
| macOS | a LaunchAgent in `~/Library/LaunchAgents` | by launchd, after 10 s |
| Linux | a `systemd --user` unit in `~/.config/systemd/user` | by systemd, after 10 s |
| Windows | a logon task in Task Scheduler (a real service needs an administrator) | by a trigger that repeats every 5 minutes |

The service listens on a loopback port picked at install, so its URL survives
restarts, and every client finds it through the runtime file. `install` takes
`--wrapper` like `--server embedded` and pins `tools.ffmpeg` to the ffmpeg on the
current `PATH`, since a service starts with a shorter one; run it again after moving the
`amdl` binary. If another backend already runs on the files, the service waits for it
to stop. `status` prints the manager's view and the running backend's PID, version, URL
and log file. `stop` holds until the next login, and on Windows until `start`, where it
ends the process outright and its jobs resume on the next start. `uninstall` removes
the registration and leaves the data. `stop` and `uninstall` with no service installed
(or, for `stop`, already stopped) return at once with a `note`, and name the PID of a
backend that `amdl serve` or the desktop app runs, which the service does not own. A `systemd --user` unit stops at logout unless
lingering is on (`loginctl enable-linger`).

## Checking a setup

`amdl doctor` checks what downloading needs and never starts a backend: the server a
command would use answers, has ffmpeg and a ready wrapper; and, when that server runs on
this machine's local files, ffmpeg is installed here (with its version). It also reports
the local data directory and service, and, when the server is in simulate mode, a
`simulate` entry (not required, so `ok` stays true) saying downloads write no file. Each failed check carries a hint, such as the
command that installs ffmpeg with the package manager found (or, without root, where to
put an ffmpeg binary and the `tools.ffmpeg` to set); any required check failing exits 5
with `checks_failed`, whose own hint repeats the failed checks' hints. With a server
that does not run on this machine's files there is no ffmpeg check here. `amdl service install` fails the same way, with
`ffmpeg_missing`, before it registers anything ([install.md](install.md#ffmpeg)).

## A wrapper amdl runs

`amdl wrapper install|update|import|uninstall|start|stop|logs` manage a wrapper that the
local backend runs in a QEMU guest (`internal/wrapperhost`); the steps, platforms and
limits are in [install.md](install.md#a-wrapper-amdl-runs). These commands work on the
local files and talk to no server. `wrapper status`, `login`, `2fa` and `logout` stay API
commands: with a managed wrapper the backend logs in through the guest, so they work as
with a wrapper-manager, and they wait up to `internal/client#LoginWait`, longer than the backend spends on one (`internal/wrapperhost#LoginLimit`).

`wrapper install` and `update` download the guest. Their progress goes to stderr, never
stdout, which keeps only the final JSON: one line rewritten in place on a terminal, else
a line at each 10% or every 15 seconds, then a line each for the sha256 check and the
unpack. `--quiet` silences it. After `wrapper uninstall` the result omits `enabled`,
which only describes an installed guest, and `log` once `--purge` deleted it; `next`
says what is left to do.

While the guest is installed, `--wrapper` does not apply to a backend that runs on the
local files: it logs that the address is ignored and keeps using the guest, and the API
refuses to change `wrapper.address` (`config_key_not_mutable`, reason `managed_wrapper`).
The refusal lasts until the backend's process starts again; `amdl system restart` keeps
the process, so it does not lift it. To use another wrapper, run `amdl wrapper uninstall`,
then `amdl service install --wrapper <address>`; or restart the service (or reopen the
desktop app), then `amdl config set wrapper.address=<address>` and `amdl system restart`. `amdl system get`
shows the address in use as `startup.wrapper_address`, and every hint about the
wrapper's address says the same.

## Agent setup

`amdl cli setup [--server name]` prints what connects a coding agent to amdl, writing
nothing: the `claude mcp add` command (with `--server` set to the one named, else to
the URL this run resolves to, so the snippet does not depend on the shell it ran in; when
nothing answers it is the default address and the result carries a `hint`; this
machine's own backend gets none, since `amdl mcp` finds it and its port can change), a Codex `config.toml` entry, an `mcpServers` JSON
entry, a paragraph for `AGENTS.md` or `CLAUDE.md`, and the agent skill
(`apps/amdl/skill/SKILL.md`): which way in to use, the download flow, the output, the
exit codes and what to do on each, and the rules an agent keeps, such as never handling
an Apple ID password. Install it with
`amdl cli setup --fields skill | jq -r .skill > ~/.claude/skills/amdl/SKILL.md`. The MCP
server's instructions repeat the skill's rules word for word
(`apps/amdl/internal/mcpserver#rules`).

## Releases

`make dist` cross-compiles `amdl` for Linux, macOS and Windows (`Makefile` lists the
platforms) into one archive each plus `SHA256SUMS`; the release workflow attaches them
to every GitHub release with the Homebrew formula and Scoop manifest rendered for them
(`deploy/packaging/README.md`). Installing from them: [install.md](install.md).
