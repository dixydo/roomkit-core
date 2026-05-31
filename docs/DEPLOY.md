# Deploy

End-to-end recipe for putting roomkit on a fresh VPS with TLS, TURN, and
recording. Tested on Ubuntu 22.04 / 24.04 (1 vCPU / 2 GB RAM is enough for
a handful of concurrent rooms).

---

## One-click installer

The fastest path on a fresh Ubuntu VPS:

```bash
curl -fsSL https://raw.githubusercontent.com/dixydo/roomkit-core/main/install.sh | bash
```

This script:
1. Prompts for your domain.
2. Installs Docker if missing.
3. Opens the firewall (ufw).
4. Downloads `docker-compose.yml` and `Caddyfile`.
5. Generates a `.env` with auto-detected public IP and random secrets.
6. Runs `docker compose up -d`.

Done. Skip to [Smoke test](#7-smoke-test).

---

## Manual deploy

### Checklist

- A VPS with a public static IP (DigitalOcean, Hetzner, Linode, AWS Lightsail…)
- A domain or subdomain pointed at that IP
- (Optional) S3-compatible bucket for permanent recording storage

### 1. Point DNS

```
meet.yourdomain.com   →  <your VPS IP>
```

Wait until `dig +short meet.yourdomain.com` returns the IP. Caddy needs
this before it can fetch a TLS cert.

### 2. Install Docker

```bash
ssh <user>@<vps>
curl -fsSL https://get.docker.com | sudo sh
sudo usermod -aG docker $USER
exit   # log out and back in so the group takes effect
```

### 3. Open the firewall

```bash
sudo ufw allow 22/tcp           # SSH
sudo ufw allow 80,443/tcp       # HTTP + HTTPS (Caddy)
sudo ufw allow 3478/udp         # TURN
sudo ufw allow 49152:49200/udp  # TURN relay range
sudo ufw enable
```

### 4. Get the config files

```bash
mkdir -p ~/roomkit && cd ~/roomkit
git clone https://github.com/dixydo/roomkit-core .
# or just download the files:
# curl -O https://raw.githubusercontent.com/dixydo/roomkit-core/main/docker-compose.yml
# curl -O https://raw.githubusercontent.com/dixydo/roomkit-core/main/Caddyfile
```

### 5. Fill in `.env`

```bash
make init-env   # copies .env.example → .env with a generated token secret
nano .env
```

Minimum required settings:

```env
DOMAIN=meet.yourdomain.com
ROOMKIT_PUBLIC_URL=https://meet.yourdomain.com
ROOMKIT_ALLOWED_ORIGINS=https://meet.yourdomain.com
```

To enable embedded TURN (recommended — without it ~15% of users behind
symmetric NAT can't connect):

```env
ROOMKIT_TURN_PUBLIC_IP=<your VPS public IP>   # curl -s ifconfig.me
ROOMKIT_TURN_SECRET=<openssl rand -hex 32>
```

To enable recording (local storage — saved to Docker volume, served at `/recordings/`):

```env
ROOMKIT_REC_ENABLED=true
ROOMKIT_PUBLIC_URL=https://meet.yourdomain.com   # used in download links
```

To use S3-compatible storage instead of local (see [Recording setup](#recording-setup)):

```env
ROOMKIT_S3_ENDPOINT=https://fra1.digitaloceanspaces.com
ROOMKIT_S3_REGION=fra1
ROOMKIT_S3_BUCKET=my-roomkit-recordings
ROOMKIT_S3_ACCESS_KEY=DO...
ROOMKIT_S3_SECRET_KEY=...
ROOMKIT_S3_PUBLIC_BASE=https://my-roomkit-recordings.fra1.cdn.digitaloceanspaces.com
```

To enable room token auth (optional; prevents anonymous joins):

```env
ROOMKIT_ROOM_TOKEN_SECRET=<openssl rand -hex 32>
```

### 6. Start

> Forks: set `IMAGE=your-org/roomkit` in `.env` to use your own Docker Hub image.

```bash
docker compose pull
docker compose up -d
docker compose logs -f
```

What you should see in the roomkit logs:

```
INFO  turn listening public-ip=... port=3478
INFO  recording enabled (local storage) workdir=/var/lib/roomkit/recordings
INFO  local recording file server enabled path=/var/lib/roomkit/recordings
INFO  WebSocket origin check enabled origins=[https://meet.yourdomain.com]
INFO  listening addr=:8080
```

### 7. Smoke test

```bash
curl -I https://meet.yourdomain.com/healthz
# HTTP/2 200, body: ok

curl https://meet.yourdomain.com/api/features
# {"recording":true,"turn":true}
```

roomkit serves no UI of its own, so there's nothing to "open" at the root URL
(`/` returns 404 — that's expected). To exercise a real call, point a roomkit
SDK client at `https://meet.yourdomain.com`. The quickest check is the bundled
demo:

```bash
# locally, against your deployed server
cd sdk && npm install && npm run build && open examples/vanilla-demo.html
# enter https://meet.yourdomain.com as the server URL, join from two tabs
```

To verify TURN works, join the same room from a phone on **mobile data** and
check `chrome://webrtc-internals` for a `relay` ICE candidate type.

---

## Recording setup

### Local storage (default)

Set `ROOMKIT_REC_ENABLED=true` and set `ROOMKIT_PUBLIC_URL` to your domain.
Recordings are muxed to `/var/lib/roomkit/recordings/` inside Docker (backed by
a named volume) and served at `https://meet.yourdomain.com/recordings/`.

Download links are broadcast to all participants once processing completes.

> **Note:** Local recording URLs have no authentication. Anyone who
> receives the URL can download the file. Use S3 mode or a proxy-level
> auth layer for sensitive content.

```bash
# Check what's been recorded
docker compose exec roomkit ls /var/lib/roomkit/recordings/
```

### S3-compatible storage (DigitalOcean Spaces, AWS S3, Cloudflare R2, MinIO…)

When `ROOMKIT_S3_BUCKET` + `ROOMKIT_S3_ACCESS_KEY` + `ROOMKIT_S3_SECRET_KEY` are
all set, S3 mode activates automatically (overrides local storage).

#### DigitalOcean Spaces setup

1. DO Console → Spaces Object Storage → **Create**
2. Region: `fra1` (or closest to your VPS)
3. Enable **CDN** for faster downloads (changes public base URL)
4. **File Listing: Restricted** is fine

Create an access key: DO Console → **API** → **Spaces Keys** → **Generate New Key** (Read + Write).

Allow public reads (two options):

- **A) Per-object ACL:** Set `ROOMKIT_S3_OBJECT_ACL=public-read`. Test with `--s3-test`.
- **B) Bucket-level:** In DO Console → Space → Settings → enable public access. Leave `ROOMKIT_S3_OBJECT_ACL` unset.

#### Verify S3 configuration

```bash
docker compose exec roomkit /usr/local/bin/roomkit --s3-test
```

Expected output:

```
--- 1. ListBuckets  → OK
--- 2. HeadBucket   → OK
--- 3. PutObject    → OK. Uploaded: https://...
--- 4. HTTP GET     → OK. End-to-end works.
```

---

## Updating

### Manual

```bash
cd ~/roomkit
docker compose pull
docker compose up -d --remove-orphans
```

### Automatic deploy on push to main

Add three GitHub secrets to the repo:

| Secret | Value |
|---|---|
| `VPS_HOST` | Your VPS IP or hostname |
| `VPS_USER` | SSH user |
| `VPS_SSH_KEY` | Private key contents (use a dedicated deploy key) |

Optional variable `VPS_DEPLOY_DIR` (defaults to `~/roomkit`).

Generate a deploy key:

```bash
ssh-keygen -t ed25519 -f ~/.ssh/roomkit-deploy -N "" -C "github-actions-deploy"
ssh-copy-id -i ~/.ssh/roomkit-deploy.pub <user>@<vps>
gh secret set VPS_HOST --body "<IP>"
gh secret set VPS_USER --body "<user>"
gh secret set VPS_SSH_KEY < ~/.ssh/roomkit-deploy
```

---

## Configuration reference

### Server flags / env vars

| Flag | Env var | Default | Purpose |
|---|---|---|---|
| `--addr` | `ROOMKIT_ADDR` | `:8080` | HTTP listen address |
| `--log-level` | `ROOMKIT_LOG_LEVEL` | `info` | `debug \| info \| warn \| error` |
| `--allowed-origins` | `ROOMKIT_ALLOWED_ORIGINS` | empty | Comma-separated origins for WS + CORS (empty = allow any, dev only) |
| — | `ROOMKIT_PUBLIC_URL` | empty | Externally reachable URL; used in local recording download links |
| — | `ROOMKIT_ROOM_TOKEN_SECRET` | empty | Enables HS256 room-token auth |
| — | `ROOMKIT_MAX_ROOMS` | `0` | Maximum concurrent rooms (0 = unlimited) |
| — | `ROOMKIT_MAX_PEERS_PER_ROOM` | `0` | Maximum peers per room (0 = unlimited) |
| `--turn-public-ip` | `ROOMKIT_TURN_PUBLIC_IP` | empty | Public IP for TURN (empty disables TURN) |
| `--turn-port` | `ROOMKIT_TURN_PORT` | `3478` | TURN UDP port |
| `--turn-secret` | `ROOMKIT_TURN_SECRET` | empty | Required when public-ip is set |
| `--turn-realm` | `ROOMKIT_TURN_REALM` | `roomkit` | TURN realm |
| `--turn-min-port` | `ROOMKIT_TURN_MIN_PORT` | `49152` | Relay UDP port range start |
| `--turn-max-port` | `ROOMKIT_TURN_MAX_PORT` | `49200` | Relay UDP port range end |
| — | `ROOMKIT_REC_ENABLED` | `false` | Enable local recording (files served at `/recordings/`) |
| — | `ROOMKIT_REC_WORKDIR` | `/tmp/roomkit-recordings` | Local path for raw + muxed files |
| — | `ROOMKIT_REC_MAX_HOURS` | `8` | Hard cap per recording session |
| — | `ROOMKIT_FFMPEG_PATH` | `ffmpeg` | Path to ffmpeg binary |
| — | `ROOMKIT_S3_BUCKET` | empty | S3 bucket name (set with KEY + SECRET to enable S3 mode) |
| — | `ROOMKIT_S3_ACCESS_KEY` | empty | S3 access key |
| — | `ROOMKIT_S3_SECRET_KEY` | empty | S3 secret key |
| — | `ROOMKIT_S3_ENDPOINT` | empty | Custom S3 endpoint (e.g. DO Spaces) |
| — | `ROOMKIT_S3_REGION` | `us-east-1` | S3 region |
| — | `ROOMKIT_S3_PUBLIC_BASE` | empty | Public URL base for uploaded files |
| — | `ROOMKIT_S3_OBJECT_ACL` | empty | e.g. `public-read`; empty for bucket-policy-controlled |
| `--s3-test` | — | — | Run S3 diagnostic and exit |

### Compose env vars (host side)

| Var | Default | Used by |
|---|---|---|
| `VERSION` | `latest` | Docker image tag |
| `DOMAIN` | `localhost` | Caddy site block |
| `LOG_LEVEL` | `info` | Passed as `--log-level` flag |

---

## Troubleshooting

### `502 Bad Gateway`

roomkit container isn't up. `docker compose logs roomkit` to see why.

### Cert not provisioned

`docker compose logs caddy` — usually DNS hasn't propagated or port 80 is
firewalled.

### WebSocket connects but no video

Check `chrome://webrtc-internals`. If `iceConnectionState` stays at `checking`,
TURN is missing or blocked. Verify TURN IP/secret are set in `.env` and port
3478/udp is open.

### Recording button not visible

`curl https://meet.yourdomain.com/api/features` — if `recording: false`, either
`ROOMKIT_REC_ENABLED` is not set to `true` or S3 vars are incomplete.

### Recording fails — no media

Ensure the browser grants camera/mic and at least one participant published
tracks before stopping the recording.

### S3 upload fails with 403

Run `--s3-test`. Common causes:
- Wrong credentials or region.
- Bucket ACL: drop `ROOMKIT_S3_OBJECT_ACL` and set bucket-wide public read in
  the DO Console instead.

### Container restart loses in-progress recordings

Only in-progress recordings are lost on restart (tracks are buffered locally
until Stop is called). Completed recordings are either uploaded to S3 or kept
in the Docker volume and unaffected by restarts.

---

## Cost reference (DigitalOcean, mid-2026)

- 1 vCPU / 2 GB VPS: **$12/mo**
- 250 GB Space + CDN: **$5/mo** (only if using S3 mode)

Comfortably covers ~50 hours of recordings per month for a small team.
