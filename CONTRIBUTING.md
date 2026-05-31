# Contributing to roomkit

Thanks for considering a contribution! roomkit aims to stay small enough to read
end-to-end in a weekend (~3K Go + ~3K TypeScript). Changes that keep it that way
are the easiest to merge.

## Project layout

| Path | What |
|---|---|
| `cmd/roomkit/` | Binary entrypoint, logger, `--s3-test` diagnostic |
| `internal/server/` | HTTP mux, CORS, `/api/*`, `/recordings/*`, lifecycle |
| `internal/signaling/` | WebSocket hub + per-client read/write pumps |
| `internal/sfu/` | Pion SFU: Manager → Room → Peer, RTP fan-out |
| `internal/turn/` | Embedded Pion TURN server (HMAC-REST auth) |
| `internal/recording/` | Per-track IVF/Ogg writers → ffmpeg mux → local/S3 |
| `internal/auth/` | Optional HS256 room-token validation |
| `internal/proto/` | Wire message types shared by signaling + sfu |
| `internal/config/` | Flag + env-var config loading |
| `sdk/`, `sdk-react/`, `sdk-vue/` | Published TypeScript client SDKs |

Architecture deep dive: [docs/ARCHITECTURE.md](docs/ARCHITECTURE.md).

## Prerequisites

| Tool | Version | Why |
|---|---|---|
| Go | 1.24+ | Backend |
| Node.js | 22+ | SDK build |
| ffmpeg | 6+ | Only for testing recording locally |
| Docker | 24+ | Only for testing the full container |

## Local development

```bash
make run            # create .env if missing, build, and run on :8080
make check          # gofmt check + go vet + go test (run before pushing)
make sdk-watch      # rebuild the vanilla SDK on save
```

To exercise a real call, build the SDK and open the bundled demo against your
running server:

```bash
make sdk-build
open sdk/examples/vanilla-demo.html   # join the same room from two tabs
```

## Before you open a PR

1. `make check` passes (gofmt, `go vet`, `go test ./...`).
2. New behaviour has a test where practical. Pure helpers (config, auth, turn,
   recording, server) are unit-tested; the SFU is exercised end-to-end via the
   browser SDKs, so signaling/SFU changes need a manual two-tab smoke test.
3. Docs are updated when behaviour or configuration changes (README, `docs/*`,
   and the relevant SDK README).
4. Keep the diff focused — one logical change per PR.

## Coding conventions

- **Go**: standard `gofmt`; errors wrapped with `%w`; structured logging via
  `log/slog`; no new third-party dependency without a clear reason.
- **TypeScript**: the SDK has no runtime dependencies — keep it that way. Public
  API changes belong in the SDK README and the `.d.ts` surface.

## Reporting bugs / requesting features

Open an issue using the templates under
[`.github/ISSUE_TEMPLATE/`](.github/ISSUE_TEMPLATE/). For anything
security-sensitive, follow [SECURITY.md](SECURITY.md) instead of filing a public
issue.

## License

By contributing you agree that your contributions are licensed under the
[MIT License](LICENSE).
