# Security Policy

## Reporting a Vulnerability

If you discover a security vulnerability in roomkit-core, **do not open a public GitHub issue**.

Report it privately by emailing the maintainers directly (see the GitHub repository contact info), or by using [GitHub's private vulnerability reporting](https://docs.github.com/en/code-security/security-advisories/guidance-on-reporting-and-writing/privately-reporting-a-security-vulnerability) if enabled on this repo.

Please include:
- Description of the vulnerability and its potential impact
- Steps to reproduce
- Affected versions
- Suggested fix (optional)

We aim to acknowledge reports within 48 hours and provide a fix within 14 days for critical issues.

## Supported Versions

Only the latest release receives security fixes.

## Security Considerations for Self-Hosters

### Authentication

- **Set `ROOMKIT_ROOM_TOKEN_SECRET`** on any server reachable from the internet. Without it, the server runs in open mode — any client can connect to any room without a token. Open mode is only appropriate for private networks or local development.
- **Use short-lived tokens.** Set `exp` 1–4 hours from the time of issue. Configure `ROOMKIT_ROOM_TOKEN_MAX_TTL_HOURS` to enforce a server-side ceiling and reject tokens with excessive lifetimes.
- **Rotate secrets** periodically. `make init-env` generates a cryptographically random secret.

### Token exposure in URLs

When auth is enabled, the JWT is passed as a `?token=` query parameter in
the WebSocket URL and in `/api/ice-config` requests. The browser WebSocket
API does not support custom headers, making this unavoidable for WS connections.

**Consequences:** Tokens appear in server access logs and browser history.

**Mitigations:**
- Use short token lifetimes (1–4 hours, enforced by `ROOMKIT_ROOM_TOKEN_MAX_TTL_HOURS`).
- Ensure your reverse proxy (Caddy / nginx) does not log full request URLs,
  or redacts the `token` parameter.
- Treat tokens as single-use where possible — issue one per join attempt.

### Network

- **Set `ROOMKIT_ALLOWED_ORIGINS`** in production. Leaving it empty disables WebSocket origin checking, which exposes the server to cross-site WebSocket hijacking (CSWSH) from any web page.
- **Do not expose the server directly to the internet without TLS.** Put roomkit behind a reverse proxy (e.g. Caddy, nginx) that terminates HTTPS/WSS.

### Recording

- When recording is enabled (`ROOMKIT_S3_*`) without room token auth, **any participant can trigger recording** of all media in the room. Either enable auth (`ROOMKIT_ROOM_TOKEN_SECRET`) and gate recording behind `record:start`/`record:stop` permissions, or restrict network access so only trusted clients can connect.
- S3 credentials in `.env` are never committed (`.gitignore` excludes `.env`). Rotate them if you suspect exposure.

### Local recording URL access

When recording is enabled in local mode (`ROOMKIT_REC_ENABLED=true`),
completed MP4s are served at `/recordings/<room>/<session>/output.mp4`
**without authentication**. The URL contains a random component but is
accessible to anyone who knows it.

Mitigations for internet-facing servers:
- Use S3 mode (`ROOMKIT_S3_*`) — presigned URLs have a configurable TTL.
- Put roomkit behind a reverse proxy that restricts `/recordings/` to
  authenticated users (e.g. Caddy `basicauth`, nginx `auth_request`).
- Set `ROOMKIT_ALLOWED_ORIGINS` and keep recording links out of chat
  messages that non-participants might see.

### TURN Server

- The embedded TURN server uses short-lived HMAC credentials (`ROOMKIT_TURN_SECRET`). Never set `ROOMKIT_TURN_PUBLIC_IP` without also setting `ROOMKIT_TURN_SECRET`.

## Resource Limits

By default roomkit allows unlimited rooms and peers. On internet-facing
servers, set limits to prevent resource exhaustion:

```env
ROOMKIT_MAX_ROOMS=50          # maximum concurrent rooms
ROOMKIT_MAX_PEERS_PER_ROOM=8  # maximum participants per room
```

Clients that exceed these limits receive a WebSocket close with
`TryAgainLater` (1013) and a descriptive error message.

Consider also placing roomkit behind a reverse proxy that enforces
per-IP connection rate limits (e.g. Caddy's `rate_limit` module or
nginx `limit_conn`).
