# Security Policy

## Supported versions

Security fixes land on the latest release and the `main` branch.

| Version             | Supported |
|---------------------|-----------|
| Latest release      | ✅        |
| `main`              | ✅        |
| Older tagged builds | ❌        |

## Reporting a vulnerability

**Do not open a public issue for security problems.**

Report privately through GitHub:

1. Go to the repository's **Security** tab → **Advisories** → **Report a vulnerability**
   (direct link: https://github.com/chalvinwz/docsli/security/advisories/new).
2. Describe the issue, the affected version (`docsli -version` or the image tag), and steps to reproduce.

This routes the report privately to the maintainer via GitHub Private
Vulnerability Reporting — no public disclosure until a fix is ready.

## What to expect

docsli is maintained by a single person on a best-effort basis. You can expect
an initial acknowledgement within a few days. Once a fix is ready it will be
released as a new tag (and a rebuilt container image), and the advisory will be
published crediting the reporter (unless you prefer to remain anonymous).

## Scope

docsli is a self-hosted MCP server that exposes a git repository of markdown
documents to authenticated AI agents. In scope:

- **Authentication / authorization** — bearer-token verification (constant-time
  compare), identity mapping, anything that lets one token act as another.
- **Path handling** — traversal out of the repo, writes touching `.git`,
  archive/validation bypasses.
- **Command injection** — anything that turns tool input into unintended git
  arguments or behavior (git runs via argv slices, never a shell).
- **Secret exposure** — tokens or document content leaking into logs, errors,
  or the mirror push.

Out of scope: vulnerabilities in git itself, Docker, or GitHub — report those
upstream. Deployments that skip TLS or expose the port publicly are
configuration issues covered in the README's security notes, not
vulnerabilities.
