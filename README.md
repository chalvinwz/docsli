# docsli

[![ci](https://github.com/chalvinwz/docsli/actions/workflows/ci.yml/badge.svg)](https://github.com/chalvinwz/docsli/actions/workflows/ci.yml)
[![Go Report Card](https://goreportcard.com/badge/github.com/chalvinwz/docsli)](https://goreportcard.com/report/github.com/chalvinwz/docsli)
[![Go Reference](https://pkg.go.dev/badge/github.com/chalvinwz/docsli.svg)](https://pkg.go.dev/github.com/chalvinwz/docsli)
[![License: MIT](https://img.shields.io/badge/License-MIT-blue.svg)](LICENSE)

**A git-backed document store for AI agents.** docsli is a small self-hosted server that lets multiple people's AI agents (Claude Code, or anything that speaks [MCP](https://modelcontextprotocol.io)) collaboratively read and write shared markdown documents — PRDs, technical proposals, ADRs — with a full, human-readable audit trail. Storage is a plain git repository: every agent write becomes a git commit attributed to that agent's owner, and every change ships with a mandatory *why* that becomes the commit message. No database, no vendor lock-in — if this server disappears tomorrow, your docs and their entire history survive as an ordinary git repo.

## How it works

```
┌─────────────┐      ┌─────────────┐      ┌─────────────┐
│ Claude Code │      │ Claude Code │      │ Claude Code │
│  (Chalvin)  │      │   (John)    │      │    (...)    │
└──────┬──────┘      └──────┬──────┘      └──────┬──────┘
       │      MCP over streamable HTTP           │
       │      Authorization: Bearer <token>      │
       ▼                    ▼                    ▼
┌─────────────────────────────────────────────────────┐
│                       docsli                        │
│  ┌──────────┐    ┌───────────┐    ┌──────────────┐  │
│  │   auth   │───▶│ mcpserver │───▶│   gitstore   │  │
│  │ token →  │    │  8 tools  │    │ git CLI via  │  │
│  │ identity │    └───────────┘    │ os/exec + 🔒 │  │
│  └──────────┘                     └──────┬───────┘  │
└──────────────────────────────────────────┼──────────┘
                                           ▼
                stored on disk    ┌──────────────────┐     async one-way push
              ────────────────▶  │  plain git repo  │  ─────────────────────▶
                                 │  *.md + git log  │      private GitHub
                                 └──────────────────┘      mirror (backup +
                                                           read-only web view)
```

- **Git is the database.** All state lives in one git repo on disk. History is `git log`, diffs are `git diff`, search is `git grep`.
- **One write = one commit.** Every `doc_create` / `doc_update` / `doc_delete` produces exactly one commit, authored as the identity behind the caller's bearer token — e.g. `Chalvin (agent) <chalvin-agent@example.com>`.
- **Mandatory why.** Every write requires a `why` explaining the change. It becomes the commit body, so `git log` reads like a design discussion.
- **Soft delete only.** Deleting moves a doc to `archive/`; nothing is ever erased from history.
- **The server is the only pusher.** An optional mirror pushes asynchronously to a private GitHub repo. Humans read there; nobody pushes back.

## MCP tools

| Tool | What it does |
|---|---|
| `doc_list` | List docs with title, metadata, created/last-edited author+date. Filter by status, tag, or type. Archive hidden unless requested. |
| `doc_read` | Full content + parsed metadata + last commit (author, date, why, revision). |
| `doc_search` | Case-insensitive full-text search across non-archived docs. |
| `doc_create` | Create a new doc. Fails if it exists. Requires `why`. |
| `doc_update` | Replace a doc's full content. Optional `expected_rev` for conflict detection. Requires `why`. |
| `doc_delete` | Soft delete into `archive/`. Requires `why`. |
| `doc_history` | Commits for one doc, newest first, with why messages. |
| `doc_diff` | Unified diff of one doc between two revisions. |

Concurrent writes are serialized server-side, and `doc_update` supports **optimistic locking**: agents pass the revision they read (`expected_rev`), and the server rejects the update with a helpful conflict message if someone changed the doc in between — no silent overwrites, and the losing agent knows exactly how to recover.

## Document metadata

Every document carries a YAML frontmatter block, validated on every write:

```markdown
---
status: in-review        # draft | in-review | approved | superseded (required)
tags: [payments, q3]     # optional
type: prd                # prd | adr | proposal | note (optional)
---

# Payment PRD
...
```

- `doc_create` without a frontmatter block auto-injects `status: draft`; invalid metadata is rejected with an error that tells the agent how to fix it.
- `doc_update` must send the frontmatter back (adjusted when the change calls for it) — updates never silently invent or drop metadata.
- `doc_list` filters on it: *"list all approved ADRs tagged payments"* is one tool call.

Deliberately **not** in frontmatter: authors and dates. Git already records who created and last edited every doc — `doc_list` exposes `created`/`created_by`/`last_modified`/`last_author` straight from commit history, so that data can never drift from reality.

## Quickstart

Requirements: Docker with Compose.

### Run the published image (recommended)

Multi-arch images (amd64 + arm64) are published to [ghcr.io/chalvinwz/docsli](https://github.com/chalvinwz/docsli/pkgs/container/docsli) — `latest` tracks main, semver tags track releases. No clone needed:

```bash
mkdir docsli && cd docsli
curl -O https://raw.githubusercontent.com/chalvinwz/docsli/main/examples/docker-compose.yml
curl -o config.yml https://raw.githubusercontent.com/chalvinwz/docsli/main/config.example.yml

$EDITOR config.yml           # map tokens to identities; openssl rand -hex 24
docker compose up -d

curl http://localhost:8080/healthz    # → ok
```

Running without a mirror? Delete the `deploy_key` mount and `GIT_SSH_COMMAND` lines from the compose file. With a mirror, put the deploy key at `./deploy_key` first (setup below).

### Run from source (development)

```bash
git clone https://github.com/chalvinwz/docsli.git && cd docsli

cp config.example.yml config.yml
$EDITOR config.yml           # generate tokens with: openssl rand -hex 24

docker compose up -d --build

curl http://localhost:8080/healthz    # → ok
```

Either way, on first start docsli initializes `./data/team-docs` as a git repo with a seed README and an `archive/` folder. That's it — the store is live.

### Connect Claude Code

On each teammate's machine (one distinct token per person):

```bash
claude mcp add --transport http docsli http://your-server:8080/mcp \
  --header "Authorization: Bearer dsl_chalvin_xxxxxxxx"
```

Then, inside a Claude Code session:

> "Create a PRD for the payments feature in docs/payment-prd.md, and check doc_history to see who last touched the roadmap."

Every write the agent makes lands as a commit under that person's configured identity:

```
$ git -C data/team-docs log --oneline
9f3c2a1 update: docs/payment-prd.md      ← John (agent)
5b81e77 create: docs/payment-prd.md      ← Chalvin (agent)
```

## Mirroring to a private GitHub repo

The mirror gives you an off-site backup plus GitHub's UI for humans to read docs and history. It is strictly one-way: docsli pushes, nobody else ever pushes to it.

1. **Create a private repo**, e.g. `github.com/you/team-docs`. Leave it empty.
2. **Create a deploy key** (an SSH key scoped to just this repo):

   ```bash
   ssh-keygen -t ed25519 -f deploy_key -N "" -C "docsli-mirror"
   ```

   In the repo: *Settings → Deploy keys → Add deploy key*, paste `deploy_key.pub`, and check **Allow write access**.
3. **Add the remote** to the data repo:

   ```bash
   git -C data/team-docs remote add origin git@github.com:you/team-docs.git
   ```

4. **Enable the mirror** in `config.yml`:

   ```yaml
   mirror:
     enabled: true
     remote: "origin"
   ```

5. **Mount the key** — uncomment the two lines in `docker-compose.yml`:

   ```yaml
   volumes:
     - ./deploy_key:/home/docsli/.ssh/id_ed25519:ro
   environment:
     GIT_SSH_COMMAND: ssh -i /home/docsli/.ssh/id_ed25519 -o StrictHostKeyChecking=accept-new
   ```

6. `docker compose up -d` again. Every commit now pushes asynchronously; a 60-second ticker retries anything that failed (network down, GitHub hiccup). A failing mirror never blocks or fails an agent's write.

Prefer HTTPS? Use a fine-grained PAT restricted to that single repo and set the remote URL to `https://x-access-token:<PAT>@github.com/you/team-docs.git`. Credentials always come from the environment or the remote URL — never from `config.yml`.

## No lock-in, by construction

The data directory is an ordinary git repository of markdown files. At any moment you can:

```bash
cd data/team-docs
git log --stat           # the full audit trail
git blame docs/prd.md    # who wrote which line, agent by agent
git checkout HEAD~5 -- docs/prd.md   # any historical version
```

Stop the server forever and you lose nothing: the repo *is* the product. Any git host, any editor, any future tool picks it up as-is.

## Deploying behind a reverse proxy (VPS / EC2)

docsli is built for TLS termination at a proxy: it binds plain HTTP on a private address and the proxy fronts it with HTTPS. In stateless MCP mode there are **no long-lived streaming connections** — every tool call is a short POST/response cycle — so default proxy timeouts just work, on nginx and AWS ALB alike.

### nginx

```nginx
server {
    listen 443 ssl;
    server_name docs.example.com;
    # ssl_certificate / ssl_certificate_key via certbot or your CA

    location / {
        proxy_pass http://127.0.0.1:8080;
        proxy_http_version 1.1;
        proxy_set_header Host $host;
        client_max_body_size 20m;   # doc content travels in JSON bodies; default 1m 413s large docs
        proxy_buffering off;         # MCP responses are event-stream framed
    }
}
```

### AWS ALB

- Target group: the one instance, port 8080, health check path `/healthz` (unauthenticated by design), expect HTTP 200.
- Default idle timeout (60s) is fine — server-side git operations are capped at 30s.
- HTTPS listener with an ACM certificate; keep port 8080 closed in the security group so only the ALB reaches the instance.

### Things to know

- **Volume ownership on Linux hosts.** The container runs as non-root uid 1000, but `docker compose` creates missing bind-mount dirs as root — git then fails with `could not lock config file .git/config: Permission denied`. Before first start:

  ```bash
  mkdir -p data && sudo chown -R 1000:1000 data
  # if mounting a mirror deploy key:
  sudo chown 1000:1000 deploy_key && sudo chmod 600 deploy_key
  ```

  (macOS Docker Desktop/OrbStack maps bind-mount permissions transparently, which is why this only bites on Linux.)
- **Scale up, not out.** The repo lives on local disk and writes are serialized in-process — run exactly one instance. Two replicas would each have their own repo and race the mirror.
- **Persistence:** keep the data volume on durable disk (EBS on EC2), and enable the GitHub mirror as an off-site backup.
- **Rate limiting** is not built in; add nginx `limit_req` or AWS WAF in front if the endpoint is internet-facing.

## Security notes

- **Run behind TLS.** docsli speaks plain HTTP; put it behind a reverse proxy (Caddy, nginx, Traefik, ALB) or keep it on a private network / VPN. Bearer tokens are transport secrets — over plain HTTP on a public network they can be sniffed.
- Tokens map to identities in `config.yml`; compare happens in constant time. Rotate by editing the file and restarting.
- The server never logs tokens or document content — only who wrote what path, when.
- Path traversal, absolute paths, non-markdown files, and anything touching `.git` are rejected on every tool call.

## Development

```bash
make test     # go test -race ./...
make lint     # golangci-lint run
make vuln     # govulncheck ./...
make build    # → bin/docsli
```

The test suite runs against real temp-dir git repos — no mocks — including a full-stack MCP-over-HTTP integration test where two authenticated agents collaborate on one doc, and a mirror test that pushes to a local bare repo.

```
cmd/docsli/          flag parsing, wiring, HTTP serving
internal/config/     config.yml loading + fail-fast validation
internal/auth/       bearer token → identity, constant-time verify
internal/gitstore/   ALL git operations behind the Store interface
internal/mcpserver/  the 8 MCP tools, descriptions written for agents
```

## Roadmap

Deliberately out of v1 — the current design keeps them possible:

- Web UI for browsing docs and history
- PR/review mode (propose on a branch, human merges)
- Branch support and richer merge-conflict handling
- Custom frontmatter schemas (owner links, ticket refs, arbitrary keys)
- Semantic / vector search
- Multi-repo support
- User-management endpoints and metrics
- Published container images (ghcr.io) and prebuilt binaries

## License

[MIT](LICENSE)
