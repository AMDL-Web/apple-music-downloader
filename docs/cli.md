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
| `amdl` | With stdout not a terminal, a short JSON list of every command; in a terminal, the same as text |
| `amdl cli search "<task>"` | The five commands that best match a task, offline: `[{command, summary, example}]` |
| `amdl cli commands` | Every command with its MCP tool and safety level |
| `amdl schema <command>` | The command's flags, positional arguments, safety, exit statuses, examples, and each API operation it calls: method, path, parameters, request body and output JSON Schema |
| `amdl <command> --help` | The same as text |

An unknown command fails with `usage_error` and `details.did_you_mean`.

## Commands

| Command | API | Safety |
| --- | --- | --- |
| `search <term> [--types songs,albums,artists] [--storefront] [--limit] [--offset]` | `GET /search` | read |
| `artists albums <artist_id> [--storefront] [--limit] [--offset]` | `GET /artists/{artist_id}/albums` | read |
| `lookup <url>` | `POST /lookup` | read |
| `quality <url>` | `POST /quality` | read |
| `download <url>… [--codec] [--codec-fallback] [--max-sample-rate] [--max-bit-depth] [--overwrite] [--wait\|--watch] [--timeout]` | `POST /downloads` | write; destructive with `--overwrite` |
| `jobs list [--status] [--type] [--q] [--sort] [--order] [--limit] [--offset]` | `GET /downloads` | read |
| `jobs get <job_id> [--view summary\|full]` | `GET /downloads/{job_id}` | read |
| `jobs watch [<job_id>] [--types] [--progress-interval]` | the job's event stream, or the overview feed | read |
| `jobs wait <job_id> [--timeout]` | the job's event stream | read |
| `jobs cancel <job_id>`, `jobs retry <job_id>` | `POST …/cancel`, `…/retry` | write |
| `jobs delete <job_id>` | `DELETE /downloads/{job_id}` | destructive |
| `library list [--kind] [--q] [--limit] [--offset]` | `GET /downloads?status=completed` | read |
| `library sync get`, `library sync reset` | `/library-sync`, `…/reset` | read, destructive |
| `logs list [filters]`, `logs tail [filters]` | `GET /logs`, `/logs/stream` | read |
| `system get`, `system health`, `system stats [--days]`, `hooks list` | `/system`, `/health`, `/stats`, `/hooks` | read |
| `config get [key…]`, `config set <key=value>…`, `config reload` | `GET`/`PUT /config`, `…/reload` | read, write, write |
| `wrapper status`, `wrapper login --username <id> --password-stdin`, `wrapper 2fa <login_id> <code>`, `wrapper logout --username <id>` | `/wrapper/*` | read, write, write, destructive |
| `context list\|add <name> <url>\|use <name>\|remove <name>`, `version` | local, `/system` | |
| `serve [--config] [--hooks] [--no-console]` | runs the backend in this process | |

`download -` reads links from stdin, any number per line. `config set` takes dotted keys
with YAML values (`download.quality_priority=[alac,aac]`). `wrapper login` reads the
password from the first line of stdin and never from the command line; a `needs_2fa`
answer carries the `login_id` for `wrapper 2fa`, which has to follow within 60 seconds.

## Output

- **stdout holds one JSON value**, the API's response body as the spec describes it,
  indented and coloured in a terminal and on one line in a pipe. A change with an empty
  answer prints nothing. `NO_COLOR` turns colour off.
- **`--fields a,b`** keeps only those keys: of the object itself when it has one of
  them, otherwise of each element of the arrays inside it (the jobs of a list, the hits of
  each search group).
- **`jobs get` prints a summary by default**: the job, `album_progress`
  (`internal/progress#Album`, the console's album bar) and one short entry per track
  (`index, title, status, quality, file_size, output_path, error`). `--view full` prints
  the API's `DownloadDetail`.
- **Streams are NDJSON.** `jobs watch <job_id>` prints each `Event` as one line,
  `item_progress` at most every `--progress-interval` per track, and ends with
  `{"type":"job_snapshot","job":…}` once the job has finished. Without an id it prints the
  overview feed's messages until interrupted. `logs tail` prints `LogEntry` lines.
- **stderr** carries notes for people only when it is a terminal: the server in use
  (`→ nas (http://…)`) and a progress line while waiting. `--quiet` drops them. Anywhere
  else stderr holds nothing but the error object.

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
| 6 | Wrapper sign-in failed | `authentication_failed`, `login_unsupported` |
| 7 | A waited-for job failed or was cancelled | `job_failed` |
| 8 | The wait ended before the job did | `wait_timeout` |
| 10 | Confirmation needed; nothing was changed | `confirmation_required` |
| 130 | Interrupted | |

A download whose links are all already queued or running is not an error: it exits 0
and prints the batch, whose results carry `existing_job_id`.

## Safety

- **Destructive commands** (`jobs delete`, `library sync reset`, `wrapper logout`,
  `download --overwrite`) ask `[y/N]` when stdin and stderr are a terminal and otherwise
  need `--yes`; without it they exit 10 before sending anything.
- **`--dry-run`** on every command that changes something prints
  `{"method","url","body"}` and sends nothing; it needs no reachable server.

## Which server

The first of these that is set wins:

1. `--server`, a URL or a saved context name.
2. `AMDL_URL`, the same.
3. The current context, saved by `amdl context add` or `amdl context use` in
   `cli.yaml` under the user config directory (`~/Library/Application Support/amdl` on
   macOS, `$XDG_CONFIG_HOME/amdl` or `~/.config/amdl` on Linux).
4. `http://127.0.0.1:18080`, when its health check answers at once (`apps/amdl/internal/cli#probe`).

Otherwise the command fails with `server_not_configured`.

## Serving

`amdl serve` runs the backend and web console in the process, as
`internal/server#Run`: `--config` (default `$AMDL_CONFIG`, else
`configs/config.yaml`) and `--hooks` (default `$AMDL_HOOKS_CONFIG`, else
`configs/hooks.yaml`), stopping on SIGINT or SIGTERM and ignoring SIGHUP.

## MCP server

`amdl mcp` serves the same capabilities to an MCP client over stdio, talking to the
server picked as above: `claude mcp add amdl -- amdl mcp --server nas`. Each capability
of `internal/capability#All` that names a tool becomes one, with the safety level as its
annotations (`readOnlyHint`, `destructiveHint`, `idempotentHint`, `openWorldHint` for the
tools that read Apple's catalog) and, for a tool that passes a request through, the
operation's parameters and body fields as its input schema. Results are JSON, as
structured content and as text; a failure is a tool error whose text is the error object
of the [Errors](#errors-and-exit-statuses) table.

- **From a search to a download takes two calls**: `search_catalog`, then
  `start_download` with the hits' `url` values. It answers at once with
  `accepted[].job_id` and `rejected[]`; links already queued or running come back
  rejected with `existing_job_id`.
- **`wait_for_job`** follows one job for at most `timeout_s` seconds
  (`apps/amdl/internal/mcpserver#maxWait`) and answers `{done, job}`, the job in the
  summary view of `jobs get`. While it waits it sends `notifications/progress` with the
  album's percent, when the call carried a progress token. On `done: false`, or when the
  client lost the request, calling it again is safe.
- **`list_jobs`** returns compact rows (`apps/amdl/internal/jobview#Row`), and
  **`get_status`** gathers the system, health, wrapper and library-sync answers.
- **No tool signs the wrapper in**: a password never passes through a model. When
  `get_status` shows no account, a person runs `amdl wrapper login` or uses the console.
