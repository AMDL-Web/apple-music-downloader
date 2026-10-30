# Docs

Reference documentation for the server, the web console and the command line. Every page states its
type on the line after its title, and `tools/repocheck` fails when a page is missing
here or its type here disagrees with the page.

| Type | Meaning |
| --- | --- |
| reference | Current facts. When a reference page and the code disagree, the code wins and the page is fixed. |
| runbook | Steps to perform. |
| decision | A settled trade-off; not edited afterwards, superseded by a newer decision. |
| record | A point-in-time measurement or audit, stamped with the commit it was verified at; a snapshot, not maintained. |

| Page | Type | What it covers |
| --- | --- | --- |
| [architecture.md](architecture.md) | reference | Process lifecycle, package responsibilities, API conventions (errors, events, streams), storage, output-path and hook rules |
| [upgrading-to-2.0.md](upgrading-to-2.0.md) | runbook | Every breaking change from 1.x to 2.0, with old and new shapes and the clients affected |
| [cli.md](cli.md) | reference | The `amdl` command line: commands, JSON output, exit statuses, confirmation, server selection, the MCP server and the terminal interface |
| [api.md](api.md) | reference | Every endpoint with curl examples, job overrides, error codes, SSE stream semantics |
| [configuration.md](configuration.md) | reference | Config file, env overrides, runtime vs startup keys, simulate mode |
| [download-pipeline.md](download-pipeline.md) | reference | Stages, retries, codec fallback, concurrency pools, memory modes, lyrics, output paths |
| [automation.md](automation.md) | reference | Job hooks and library sync |
| [install.md](install.md) | runbook | Installing the `amdl` binary on macOS, Linux and Windows: install scripts, Homebrew and Scoop, ffmpeg, the wrapper options, the background service, upgrade and uninstall |
| [deployment.md](deployment.md) | runbook | Docker, mounts, `PUID`/`PGID`, config seeding, releases and image tags |
| [benchmarks.md](benchmarks.md) | record | Measured post-decrypt and end-to-end results |
| [references.md](references.md) | reference | Outside practices this repo adopted, adapted or declined, and where they landed |

New pages get a row here in the same commit. Agent-facing rules live elsewhere:
[AGENTS.md](../AGENTS.md), [REVIEW.md](../REVIEW.md), [GLOSSARY.md](../GLOSSARY.md)
and `.claude/`.
