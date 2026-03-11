# AnchorDB

AnchorDB is a local code-memory service for Git repositories.

It stores durable notes attached to files, spans, and symbols, then exposes that data through:

- `anchord` for HTTP and the web viewer
- `anchorctl` for shell workflows and scripts
- `anchordb-mcp` for MCP hosts such as Claude Code

AnchorDB is primarily useful as durable working memory for coding agents. It gives an agent or a human a place to store code-local warnings, debugging notes, TODOs, handoff context, and rationale that should stay attached to the implementation instead of disappearing into chat history.

## Use Cases

AnchorDB is best suited for:

- AI agents that need to read context before editing a file or symbol
- agent handoffs where one run should leave precise notes for the next run
- long debugging sessions where repros and findings should stay attached to code
- human review notes on risky paths such as billing, auth, migrations, or retry logic
- local or team workflows that want both a web viewer and an MCP surface over the same code memory

The main workflows are:

- `anchordb-mcp` for coding agents
- `anchorctl` for scripts, shells, and CI
- `anchord` for browsing code, anchors, comments, and diffs in a browser

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

Built-in symbol extractors:

- Go
- Python
- JavaScript
- TypeScript

## Install

Install the binaries into your Go bin directory:

```bash
go install ./cmd/anchord
go install ./cmd/anchorctl
go install ./cmd/anchordb-mcp
```

If this project is published under a module path later, the same pattern becomes:

```bash
go install <module-path>/cmd/anchord@latest
go install <module-path>/cmd/anchorctl@latest
go install <module-path>/cmd/anchordb-mcp@latest
```

Check where Go installs binaries with:

```bash
go env GOBIN
go env GOPATH
```

## Quick Start

Start the server:

```bash
anchord -db ./anchor.db -sync-interval 30s
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
anchordb-mcp --db ./anchor.db
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
anchordb-mcp --db ./anchor.db
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

AnchorDB stores data in SQLite.

The same database can be used by:

- `anchord`
- `anchorctl`
- `anchordb-mcp`

Typical local path:

```text
./anchor.db
```
