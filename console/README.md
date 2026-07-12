# Tarisya Console

Tarisya Console is the React and TypeScript web interface for Tarisya. It is
developed with Vite and compiled into static assets that are embedded in the
Tarisya Core binary for production releases.

For product installation and self-hosting instructions, see the
[project README](../README.md).

## Development

Start Tarisya Core from the repository root:

```bash
go run ./cmd/core
```

In another terminal, start the Console development server:

```bash
cd console
pnpm install --frozen-lockfile
pnpm dev
```

Open the URL printed by Vite. Development requests under `/api` are forwarded
to Core at `http://localhost:8081`, so authentication cookies and API calls use
the same paths as the embedded production application.

## Checks

```bash
pnpm lint
pnpm build
```

`pnpm build` creates `console/dist`. The repository-level build script copies
those files into `internal/webui/dist` before compiling Core:

```bash
./scripts/build.sh
```

Run that command from the repository root. Both `console/dist` and
`internal/webui/dist` are generated output and should not be committed.

## API client

The production Console uses the same-origin API base `/api/v1`. During local
development, Vite proxies `/api` to Core. A different API base can be supplied
at build time with `VITE_API_BASE_URL`, although same-origin deployment is the
recommended configuration for session-cookie authentication.
