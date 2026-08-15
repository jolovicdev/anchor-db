# AnchorDB

**Code memory for AI coding agents.** AnchorDB is an MCP server that stores notes
attached to specific code — a file, a line range, a symbol — and keeps them
attached as that code moves. The context an agent builds up in one session is
still there in the next one.

Point Claude Code, Cursor, or any Model Context Protocol host at it, and an agent
can read *why* a workaround exists before editing it, and leave notes for whoever
comes next. Everything stays local: one SQLite file, your git repositories, no
network calls, no account.

```bash
curl -fsSL https://raw.githubusercontent.com/jolovicdev/anchor-db/master/install.sh | sh
claude mcp add anchordb -- anchordb-mcp --db ~/.anchordb/anchor.db
```

No account, no API key, and nothing to run in the background. `git` is the only
requirement; prebuilt binaries cover Linux, macOS, and Windows, and there is a
[container image](#with-docker) and a [Go install](#with-go) path too.

Three ways in, over the same data:

| | |
|---|---|
| `anchordb-mcp` | MCP server for coding agents. Talks to SQLite directly; no daemon needed. |
| `anchorctl` | Command-line client for shells, scripts, and CI. |
| `anchord` | HTTP API and web viewer for reading code and notes in a browser. |

## Why

Chat history is a bad place to keep what you learn about a codebase. It scrolls
away, it is not attached to anything, and the next session starts blank.

A comment in the source is better, but you cannot leave one everywhere, and much
of what matters does not belong in the file: a reproduction for a bug you did not
fix, why the obvious refactor is wrong, what broke the last time someone touched
this retry loop, where you stopped.

AnchorDB keeps those notes beside the code instead of inside it, and follows the
code when it moves. A note pinned to a function is still on that function after
you rename it, reorder the file, or move it to another package.

## Use cases

- Coding agents that should read context before editing a file or symbol
- Agent handoffs, where one run leaves precise notes for the next
- Long debugging sessions, keeping repros and findings attached to the code
- Review notes on risky paths — billing, auth, migrations, retry logic
- Any codebase where the reason for a decision outlives the person who made it

## How It Works

AnchorDB stores `anchors`.

Each anchor contains:

- repo metadata
- a note body and kind
- a file path
- a line and column range
- selected text plus surrounding context
- an optional symbol path

When files change, AnchorDB re-resolves anchors using the saved span, text context, and symbol information. Tree-sitter improves symbol extraction and relocation, but the system still works without it.

Resolution tries strategies from most to least certain and stops at the first
that holds up:

1. **Exact span** — the recorded lines still hold the recorded text.
2. **Git line mapping** — each anchor records the commit its line numbers were
   taken against, so `git diff` says exactly where those lines went. This is
   deterministic where text matching can only guess, and it is the difference
   between finding the right copy of a duplicated block and finding the first
   one. The mapping is always verified against the stored text before it is
   applied, so it degrades safely rather than mis-anchoring.
3. **Symbol match** — the same symbol path, scored on how much of its body
   survived.
4. **Text and context match** — the recorded text found elsewhere in the file,
   ranked by surrounding context.

An anchor that none of these can place is marked `stale` and keeps its last
known position. Because the diff is available, a stale anchor can say whether
its code was *rewritten in place* or *deleted outright*. Resolution is per-path
and failures are isolated, so one unresolvable file never stops the rest of a
repo from syncing.

Anchors written before this existed have no recorded base commit; they simply
skip the git step and fall back to text matching.

## Triage

Stale anchors are a queue, not a dead end. AnchorDB ranks the places each one
might now belong -- with a confidence score and a code preview -- and lets you
accept one:

```bash
anchorctl anchor stale --repo-id repo_123
anchorctl anchor candidates --id anchor_123
anchorctl anchor relocate --id anchor_123 --candidate 0
```

Relocating re-reads the span from the file, re-derives the symbol at the new
position, and re-bases the anchor onto the current commit, so the next automatic
pass can follow it through git again. To re-pin somewhere the suggestions
missed, give an explicit range instead:

```bash
anchorctl anchor relocate --id anchor_123 --start-line 42 --end-line 58
```

The same loop is available to agents over MCP (`anchor_stale`,
`anchor_candidates`, `anchor_relocate`), so a run that refactors code can re-pin
the notes it left behind. In the web viewer it appears as a review panel on each
stale anchor.

## History

Every move, stale, and update is recorded with its reason and confidence, and
that history is readable everywhere:

```bash
anchorctl anchor events --id anchor_123
```

It answers "why is this note here?" — for example `created`, then
`moved · git line mapping`, then `stale · the anchored lines were deleted or
restructured`. Resolution passes that change nothing record nothing, so the
history stays signal.

Paths are always interpreted as repo-relative and confined to the repository:
requests that try to escape the root, follow a symlink out of it, or read `.git`
are rejected. Git refs that would be parsed as command-line options are refused
for the same reason.

Built-in symbol extractors:

- Go
- Python
- JavaScript
- TypeScript

## Install

`git` is the only runtime requirement. Every release ships prebuilt binaries for
Linux, macOS, and Windows, so a Go toolchain is only needed if you build from
source.

Only `anchordb-mcp` is needed to use AnchorDB from a coding agent. `anchorctl`
and `anchord` add the command-line client and the web viewer.

### Download a binary

Linux and macOS:

```bash
curl -fsSL https://raw.githubusercontent.com/jolovicdev/anchor-db/master/install.sh | sh
```

That fetches the archive for your platform, verifies it against the release
checksums, and installs into `~/.local/bin`. Set `ANCHORDB_INSTALL_DIR` to put it
somewhere else, or `ANCHORDB_VERSION` to pin a version.

To do it by hand instead, take the archive for your platform from the
[releases page](https://github.com/jolovicdev/anchor-db/releases), unpack it, and
move the binaries onto your `PATH`. `checksums.txt` in each release covers every
archive.

### With Go

Requires Go 1.25 or newer.

```bash
go install github.com/jolovicdev/anchor-db/cmd/anchordb-mcp@latest
go install github.com/jolovicdev/anchor-db/cmd/anchorctl@latest
go install github.com/jolovicdev/anchor-db/cmd/anchord@latest
```

From a local checkout:

```bash
go install ./cmd/anchordb-mcp ./cmd/anchorctl ./cmd/anchord
```

If the installed command is not found afterwards, the Go bin directory is not on
your `PATH`:

```bash
go env GOBIN          # if empty, binaries are in $(go env GOPATH)/bin
export PATH="$PATH:$(go env GOPATH)/bin"
```

### With Docker

```bash
docker run --rm -i \
  -v ~/.anchordb:/data \
  -v /path/to/repo:/path/to/repo \
  ghcr.io/jolovicdev/anchor-db:latest
```

Anchors record absolute repository paths, so mount each repository at the same
path it has on the host — otherwise the paths stored in the database will not
match anything the container can see. The container is only worth the trouble in
CI or a sandbox; a binary on the host is simpler everywhere else.

### Verify

```bash
anchordb-mcp --version
```

### Where to keep the database

One database holds many repositories. A stable per-user location works well:

```bash
mkdir -p ~/.anchordb
```

Use `~/.anchordb/anchor.db` in the commands below. Keep it out of your
repositories — it is local state, not source, and does not belong in git.

## Install with a coding agent

If you would rather have an agent do this, point it at this repository and say
"install this". The steps below are written to be followed directly.

**1. Check prerequisites.** `go version` (needs 1.25+) and `git --version`. If Go
is missing, stop and ask before installing a toolchain.

**2. Install the MCP server.**

```bash
go install github.com/jolovicdev/anchor-db/cmd/anchordb-mcp@latest
```

**3. Find the binary.** `go env GOBIN`, or `$(go env GOPATH)/bin` if that is
empty. Use the absolute path in step 5 rather than editing shell profiles.

**4. Choose a database path.** `mkdir -p ~/.anchordb` and use
`~/.anchordb/anchor.db`. Do not put it inside the user's repository.

**5. Register the server.** For Claude Code:

```bash
claude mcp add anchordb -- /absolute/path/to/anchordb-mcp --db /absolute/path/to/anchor.db
```

For any host using `mcpServers` JSON, merge — do not overwrite — the entry shown
under [Claude Code Setup](#claude-code-setup). Absolute paths only; most hosts do
not expand `~`.

**6. Verify.** `anchordb-mcp --version` should print a version. Restart the host
and confirm `anchor_context` appears in the tool list. If it does not, the config
was not picked up: check you edited the config for the host that is running, and
that it fully restarted.

**7. Register the repository** with the `repo_add` tool, or:

```bash
anchorctl repo add --name <name> --path /path/to/repo
```

Report the returned repo ID to the user; most commands take it.

### Using it well

Read before editing. Call `anchor_context` with the repo ID and file path before
modifying a file. Anchors record what the code does not say — why a workaround
exists, which invariant a function holds, what broke last time. Skipping that is
how the same bug gets reintroduced.

Write what is durable and non-obvious. Good anchors: a constraint the types do
not enforce, why an appealing simplification is wrong, a reproduction for a bug
you did not fix, handoff state when stopping mid-task. Set `author` to identify
yourself, for example `agent://claude`.

Skip what the code already says. A note restating a function signature is noise,
and noise buries the anchors that matter.

Re-pin what you break. After a refactor, call `anchor_stale` for the repo. Use
`anchor_candidates` to see suggested new locations and `anchor_relocate` to
accept one, or give an explicit line range if you know better. Leaving stale
anchors behind makes the next run worse than the last.

Do not commit the database, and do not delete anchors you did not create without
asking.

## Quick Start

Start the server:

```bash
anchord --db ~/.anchordb/anchor.db
```

Register a repo:

```bash
anchorctl repo add --name demo --path /path/to/repo
```

Create an anchor:

```bash
anchorctl anchor create \
  --repo-id repo_123 \
  --ref WORKTREE \
  --path internal/service/run.go \
  --start-line 42 \
  --start-col 1 \
  --end-line 49 \
  --end-col 2 \
  --kind warning \
  --title "Retry must stay idempotent" \
  --body "This path duplicated writes during incident 2026-02-14." \
  --author human://alice
```

Open the viewer:

```text
http://127.0.0.1:7740/
```

Start the MCP server:

```bash
anchordb-mcp --db ~/.anchordb/anchor.db
```

## CLI

`anchorctl` talks to the running HTTP server.

It reads the base URL from `ANCHOR_DB_URL`. Default:

```text
http://127.0.0.1:7740
```

### Repo Commands

Add a repo:

```bash
anchorctl repo add --name demo --path /path/to/repo
```

List repos:

```bash
anchorctl repo list
```

Get one repo:

```bash
anchorctl repo get --id repo_123
```

Sync one repo:

```bash
anchorctl repo sync --id repo_123
```

Remove one repo:

```bash
anchorctl repo remove --id repo_123
```

### Anchor Commands

List anchors:

```bash
anchorctl anchor list --repo-id repo_123 --path internal/api/server.go --limit 20 --offset 0
```

Get one anchor:

```bash
anchorctl anchor get --id anchor_123
```

Create an anchor:

```bash
anchorctl anchor create \
  --repo-id repo_123 \
  --ref WORKTREE \
  --path internal/api/server.go \
  --start-line 40 \
  --start-col 1 \
  --end-line 52 \
  --end-col 2 \
  --kind warning \
  --title "Keep request validation strict" \
  --body "This handler should reject empty repo_id values." \
  --author human://alice
```

Update anchor metadata:

```bash
anchorctl anchor update \
  --id anchor_123 \
  --kind handoff \
  --title "Next step" \
  --body "Trace the retry path before changing the timeout logic." \
  --author agent://planner \
  --tags billing,handoff
```

Archive an anchor:

```bash
anchorctl anchor close --id anchor_123
```

Reopen an anchor:

```bash
anchorctl anchor reopen --id anchor_123
```

Re-run anchor resolution:

```bash
anchorctl anchor resolve --id anchor_123
```

### Context, Comments, Search

Get file or symbol context:

```bash
anchorctl context --repo-id repo_123 --ref WORKTREE --path internal/api/server.go --symbol "*Server.handleRepos"
```

List comments:

```bash
anchorctl comment list --anchor-id anchor_123
```

Add a comment:

```bash
anchorctl comment add --anchor-id anchor_123 --author human://alice --body "Confirmed in production replay."
```

Full-text search:

```bash
anchorctl search --query retry --repo-id repo_123 --path internal/api/server.go
```

Queries are treated as literal text, so punctuation-heavy terms such as `C++`,
`don't`, or `retry (v2)` are safe to search for. Multiple terms are ANDed, and a
trailing `*` performs a prefix search:

```bash
anchorctl search --query "retry idempot*" --repo-id repo_123
```

All CLI commands return JSON.

## HTTP API

Default listen address:

```text
http://127.0.0.1:7740
```

### Endpoints

- `GET /health`
- `GET /v1/repos`
- `POST /v1/repos`
- `GET /v1/repos/{repo_id}`
- `POST /v1/repos/{repo_id}/sync`
- `DELETE /v1/repos/{repo_id}`
- `GET /v1/anchors`
- `POST /v1/anchors`
- `GET /v1/anchors/{anchor_id}`
- `PATCH /v1/anchors/{anchor_id}`
- `POST /v1/anchors/{anchor_id}/close`
- `POST /v1/anchors/{anchor_id}/reopen`
- `POST /v1/anchors/{anchor_id}/resolve`
- `GET /v1/anchors/{anchor_id}/comments`
- `POST /v1/anchors/{anchor_id}/comments`
- `GET /v1/anchors/{anchor_id}/events`
- `GET /v1/anchors/{anchor_id}/candidates`
- `POST /v1/anchors/{anchor_id}/relocate`
- `GET /v1/stale`
- `GET /v1/context`
- `GET /v1/search`
- `GET /view`

### Common Requests

Create a repo:

```http
POST /v1/repos
Content-Type: application/json

{
  "name": "demo",
  "path": "/path/to/repo"
}
```

Response:

```json
{
  "id": "repo_123",
  "name": "demo",
  "root_path": "/path/to/repo",
  "default_ref": "abc123...",
  "created_at": "2026-03-11T10:00:00Z",
  "updated_at": "2026-03-11T10:00:00Z"
}
```

Create an anchor:

```http
POST /v1/anchors
Content-Type: application/json

{
  "repo_id": "repo_123",
  "ref": "WORKTREE",
  "path": "internal/service/run.go",
  "start_line": 42,
  "start_col": 1,
  "end_line": 49,
  "end_col": 2,
  "kind": "warning",
  "title": "Retry must stay idempotent",
  "body": "This path duplicated writes during incident 2026-02-14.",
  "author": "human://alice",
  "tags": ["billing", "warning"]
}
```

Update an anchor:

```http
PATCH /v1/anchors/anchor_123
Content-Type: application/json

{
  "kind": "handoff",
  "title": "Next step",
  "body": "Check the retry path before changing timeout handling.",
  "author": "agent://planner",
  "tags": ["billing", "handoff"]
}
```

Close, reopen, or resolve an anchor:

```http
POST /v1/anchors/anchor_123/close
POST /v1/anchors/anchor_123/reopen
POST /v1/anchors/anchor_123/resolve
```

Read file context:

```http
GET /v1/context?repo_id=repo_123&ref=WORKTREE&path=internal/service/run.go&symbol=*Runner.Run
```

Full-text search:

```http
GET /v1/search?query=retry&repo_id=repo_123&path=internal/service/run.go&limit=20&offset=0
```

Add a comment:

```http
POST /v1/anchors/anchor_123/comments
Content-Type: application/json

{
  "author": "human://alice",
  "body": "Confirmed in staging replay."
}
```

All API responses are JSON. Validation failures return:

```json
{
  "error": "message"
}
```

## MCP

`anchordb-mcp` serves the same data over MCP stdio and reads the SQLite database directly. It does not require `anchord` to be running.

Run it:

```bash
anchordb-mcp --db ~/.anchordb/anchor.db
```

### Claude Code Setup

On Linux or WSL, Anthropic currently documents two install paths for Claude Code:

- native installer: `curl -fsSL https://claude.ai/install.sh | bash`
- npm installer: `npm install -g @anthropic-ai/claude-code`

After Claude Code is installed, add AnchorDB as a stdio MCP server:

```bash
claude mcp add anchordb --scope project -- /absolute/path/to/anchordb-mcp --db /absolute/path/to/anchor.db
```

Useful follow-up commands:

```bash
claude mcp list
claude mcp get anchordb
```

Inside Claude Code, use `/mcp` to inspect configured MCP servers and their status.

Notes:

- `--scope project` stores the configuration in `.mcp.json` for the current project
- `--scope local` keeps it private to your local project setup
- `--scope user` makes it available across projects on your machine
- everything after `--` is the actual server command and its arguments

Equivalent `.mcp.json` entry:

```json
{
  "mcpServers": {
    "anchordb": {
      "command": "/absolute/path/to/anchordb-mcp",
      "args": ["--db", "/absolute/path/to/anchor.db"]
    }
  }
}
```

Once connected, a coding agent can:

- read anchor context before editing a file
- search previous notes and comments
- create or update anchors during debugging
- leave handoff notes for the next run

### MCP Tools

- `repo_add`
- `anchor_repos`
- `repo_get`
- `repo_sync`
- `repo_remove`
- `anchor_context`
- `anchor_create`
- `anchor_update`
- `anchor_close`
- `anchor_reopen`
- `anchor_resolve`
- `anchor_comment`
- `anchor_search`
- `anchor_text_search`
- `anchor_events`
- `anchor_stale`
- `anchor_candidates`
- `anchor_relocate`
- `anchor_get`
- `anchor_comments`
- `anchor_file_view`

### MCP Resources

- `anchordb://repos`
- `anchordb://repo/{repo_id}`
- `anchordb://context/{repo_id}{?ref,path,symbol}`
- `anchordb://search{?query,repo_id,path,symbol,kind,limit,offset}`
- `anchordb://anchors/{repo_id}{?path,symbol,status,limit,offset}`
- `anchordb://file/{repo_id}{?ref,path}`
- `anchordb://events/{anchor_id}`
- `anchordb://anchor/{anchor_id}`
- `anchordb://comments/{anchor_id}`

## Viewer

The web viewer shows:

- a repo file list
- the selected file with highlighted anchor ranges
- anchor cards and threaded comments
- the Git working-tree diff for that file

Highlighted lines mark anchor coverage. The diff panel shows the actual Git diff for the selected file.

## Storage

Everything lives in one SQLite database — anchors, comments, history, and the
full-text search index. No server, no external dependency, nothing leaves the
machine.

All three binaries read the same file:

```bash
anchord --db ~/.anchordb/anchor.db
anchorctl repo list                 # via anchord
anchordb-mcp --db ~/.anchordb/anchor.db
```

Keep it outside your repositories. It is local state, not source.

Back it up by copying the file while nothing is writing, or with
`sqlite3 anchor.db ".backup anchor-backup.db"`.

## Troubleshooting

| Symptom | Cause | Fix |
|---|---|---|
| `command not found: anchordb-mcp` | Go bin directory not on `PATH` | `export PATH="$PATH:$(go env GOPATH)/bin"`, or use absolute paths |
| MCP host does not list the server | Config not reloaded | Restart the host fully; check `claude mcp list` |
| `not a git repository` | Path is not a git checkout | `git init`, or point at the actual repository root |
| `path escapes repository root` | Path outside the repo, or a symlink leaving it | Use a repository-relative path |
| `invalid git ref` | Ref begins with `-` | Use a commit SHA, a branch name, or `WORKTREE` |
| Anchors show as `stale` after a refactor | The code moved beyond automatic matching | Run the triage loop: `anchorctl anchor stale` |
| `connection refused` from `anchorctl` | `anchord` is not running | Start it, or use the MCP tools, which need no daemon |
| Search returns nothing for an exact phrase | Terms are ANDed | Try fewer terms, or a prefix: `retry idempot*` |

## Versioning

AnchorDB follows semantic versioning. All three binaries report the same
version:

```bash
anchord --version
anchorctl version
anchordb-mcp --version
```

The database schema migrates forward automatically on open. Anchors created by
earlier versions keep working; those written before git-aware resolution simply
fall back to text matching until they next resolve cleanly.

Each tagged release builds its binaries on the platform they target, publishes a
`checksums.txt` covering every archive, and pushes a matching multi-architecture
image to `ghcr.io/jolovicdev/anchor-db`.

## License

MIT. See [LICENSE](LICENSE).
