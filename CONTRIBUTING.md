# Contributing to Tarisya

Thanks for your interest in improving Tarisya. This guide covers the project
layout, local setup, expected workflow, and checks required before opening a
pull request.

## Project layout

See [Components](README.md#components) for an overview of Core, Console, and
Agent. The main source directories are:

- `cmd/` — executable entry points
- `internal/` — Core, Agent, administration, and embedded UI packages
- `console/` — React and TypeScript Console source
- `migrations/` — embedded SQLite migrations
- `scripts/` — build and Linux installation scripts

## Getting set up

Requirements: [Go 1.24+](https://go.dev), [Node 24+](https://nodejs.org), pnpm
10, and Git.

```bash
git clone https://github.com/mhmdnurf/tarisya.git
cd tarisya
cp .env.example .env
./scripts/build.sh
```

For day-to-day development, see [Development](README.md#development). Core,
the Console development server, and the Agent typically run in separate
terminals.

## Making changes

- Keep pull requests focused on one change and avoid unrelated refactors.
- Follow idiomatic Go, handle errors explicitly, run `gofmt`, and avoid
  unnecessary abstractions.
- Add both `.up.sql` and `.down.sql` migrations when changing the database
  schema; do not edit migrations that have already been released.
- Run the Console lint and production build when changing frontend code.
- Update documentation when changing configuration, CLI commands, deployment,
  or the API surface.
- Add or update tests when application behavior changes.

## Testing

Use the complete build as the final clean-clone check. It installs Console
dependencies, builds and stages the embedded UI, runs Go tests, and compiles
the Core and Agent binaries:

```bash
./scripts/build.sh
git diff --check
```

For Console-only changes:

```bash
cd console
pnpm install --frozen-lockfile
pnpm lint
pnpm build
```

For Go-only changes after Console assets have been staged:

```bash
gofmt -w path/to/changed.go
go test ./...
```

## Commit messages

Use clear, imperative commit messages. For example:

```text
fix(agent): retry transient network failures
```

Reference related issues where relevant.

## Submitting a pull request

1. Fork the repository and create a branch from `main`.
2. Make a focused change with tests where applicable.
3. Run `./scripts/build.sh` and `git diff --check`.
4. Open a pull request explaining what changed and why.

## Reporting bugs and requesting features

Open a [GitHub issue](https://github.com/mhmdnurf/tarisya/issues) with:

- what you expected and what happened;
- reproduction steps for bugs;
- relevant sanitized logs, such as `journalctl -u tarisya-core` or
  `tarisya doctor` output; and
- your OS and deployment method.

Never include passwords, API keys, session cookies, JWT secrets, private
configuration, or other credentials. Report security vulnerabilities privately
through [GitHub Security Advisories](https://github.com/mhmdnurf/tarisya/security/advisories/new).

## Code of Conduct

This project follows the [Code of Conduct](CODE_OF_CONDUCT.md). By
participating, you are expected to uphold it.
