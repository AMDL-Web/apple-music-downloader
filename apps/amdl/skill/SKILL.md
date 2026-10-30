---
name: amdl
description: Download music from Apple Music with the amdl command line or the amdl MCP server - search the catalog, start and follow downloads, check jobs, the wrapper and the local backend. Use when asked to find, download or check Apple Music albums, songs, playlists or artists with amdl, when an amdl command exits non-zero, or when setting amdl up for an agent.
---

# amdl

One amdl server, two ways in:

- **MCP tools** (`search_catalog`, `start_download`, `wait_for_job`, …) when this client has the amdl MCP server: use them in a conversation.
- **The `amdl` command** otherwise, and in shells, scripts and CI. `amdl cli search "<task>"` finds the command for a task; `amdl schema <command>` shows its request, output and exit codes.

## Download

Find music with search_catalog (or list_artist_albums for an artist hit), pass the hits' url values to start_download, then follow each job_id with wait_for_job, calling it again while it answers done=false.

On the command line:

1. `amdl search "<terms>" --types albums,songs`, then `amdl artists albums <artist_id>` for an artist hit; `amdl lookup <url>` previews a pasted link.
2. `amdl download <url>...` starts jobs. A link already queued or downloaded converges on its job instead of starting a duplicate.
3. `amdl jobs wait <job_id> --timeout 30m` follows one; `amdl download <url> --wait` does both. Files land on the server's disk.

## Output

- stdout is the result as JSON, the API's own response, and nothing else. `--fields a,b` keeps those keys.
- stderr has progress notes and, on failure, `{"error":{"code","message","hint","details"}}`. The exit status is the class of failure; `error.code` is the exact cause, `error.hint` the next command.

## Before you change anything

- `--dry-run` prints the request and sends nothing.
- A destructive command (delete, logout, reset) stops with exit 10 unless it has `--yes`. Ask the user before you add `--yes`.
- Do not route around a refusal. Exit 10, exit 6 and a wrapper that is not signed in are the user's to resolve.

## Exit codes

| Code | Meaning | What to do |
| --- | --- | --- |
| 0 | Done | |
| 1 | Internal error | Report `error.message` |
| 2 | Bad input | Fix the arguments: `amdl schema <command>` |
| 3 | Not found | Check the id |
| 4 | Conflict, such as a login already pending | Wait, or finish what is pending |
| 5 | Server, wrapper or a local tool unavailable | `amdl doctor`, then follow its hints |
| 6 | Apple refused the wrapper's sign-in | Tell the user; see below |
| 7 | The job failed | `amdl jobs get <job_id>` shows each track's error; `amdl jobs retry <job_id>` |
| 8 | The wait timed out; the job goes on | Wait again |
| 10 | Needs confirmation | Ask the user; add `--yes` only if they agree |
| 130 | Interrupted | |

## The wrapper and the Apple ID

Signing in to Apple is done by a person, never by an agent: do not ask for, type, read or store an Apple ID password or two-step code.

When get_status (or `amdl doctor`) says the wrapper is not signed in, tell the user to run `amdl wrapper login` in their own terminal, which asks for the password and any two-step code without echoing them, or to use the web console's Account page.

A machine with no wrapper can run one: `amdl wrapper install`, then `amdl service install`. It downloads about 65 MB and may install QEMU, so let the user decide.

## Setup and diagnosis

- `amdl doctor` checks the server, its ffmpeg and wrapper, and ffmpeg here; every failure carries a hint.
- The server is `--server`, else `AMDL_URL`, else the current context (`amdl context list`), else this machine's backend.
- `amdl cli setup` prints the MCP configuration for Claude Code, Codex and other clients, and this skill.
