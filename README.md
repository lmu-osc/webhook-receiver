# webhook-receiver

Small Go service that receives GitHub push webhooks, validates the signature, and keeps a target branch checked out and updated on disk.

This repo is set up to run well on a Debian server using Docker and Docker Compose, with Nginx terminating TLS on port 443.

## What it does

- Exposes `POST /webhook`
- Verifies `X-Hub-Signature-256` with `WEBHOOK_SECRET`
- Only reacts to `push` events for `TARGET_REF`
- Clones the configured repo on first event
- Runs `git pull` on subsequent matching events
- Coalesces overlapping webhook events so updates do not run in parallel

## Project files

- `Dockerfile`: multi-stage build for a small runtime image
- `.dockerignore`: excludes local artifacts and secrets from image build context
- `docker-compose.yml`: container runtime config (port, env file, volume)
- `main.go`, `handler_webhook.go`: webhook server and git update logic

## Debian server setup

### 1. Install Docker + Compose plugin

(See official Docker docs for installation.)

Optional: run Docker without sudo.

```bash
sudo usermod -aG docker "$USER"
# Log out and log back in after this.
```

### 2. Clone this repository

```bash
git clone https://github.com/lmu-osc/webhook-receiver.git
cd webhook-receiver
```

### 3. Configure environment

Create or edit `.env` in the repo root.

Required:

- `WEBHOOK_SECRET`: random hex/string used to validate webhook signature
- `REPO_URL`: repository to mirror locally

Optional (defaults shown):

- `REPO_DIR=./repo`
- `TARGET_REF=refs/heads/gh-pages`
- `TARGET_BRANCH=gh-pages`
- `SERVE_PORT=8080`

Example `.env` file:

```dotenv
WEBHOOK_SECRET=replace-with-a-long-random-secret
REPO_URL=https://github.com/lmu-osc/lmu-osc.github.io.git
REPO_DIR=./repo
TARGET_REF=refs/heads/gh-pages
TARGET_BRANCH=gh-pages
SERVE_PORT=8080
```

You can quickly generate a strong secret with openssl:

```bash
openssl rand -hex 32
```

### 4. Start the service

```bash
docker compose up -d --build
```

Check status/logs:

```bash
docker compose ps
docker compose logs -f webhook-receiver
```

Stop/restart:

```bash
docker compose down
docker compose up -d
```

### 5. Route `/webhook` through Nginx on 443

The Compose file binds the app port to `127.0.0.1` only, so it is reachable from Nginx on the same host but not directly from the internet.

Example Nginx server block:

```nginx
server {
	listen 443 ssl http2;
	server_name webhook.example.com;

	ssl_certificate /etc/letsencrypt/live/webhook.example.com/fullchain.pem;
	ssl_certificate_key /etc/letsencrypt/live/webhook.example.com/privkey.pem;

	location = /webhook {
		proxy_pass http://127.0.0.1:8080/webhook;
		proxy_http_version 1.1;
		proxy_set_header Host $host;
		proxy_set_header X-Forwarded-For $proxy_add_x_forwarded_for;
		proxy_set_header X-Forwarded-Proto $scheme;
	}

	location / {
		return 404;
	}
}
```

If your `.env` sets a different `SERVE_PORT`, update `proxy_pass` accordingly.

Validate and reload Nginx:

```bash
sudo nginx -t
sudo systemctl reload nginx
```

## GitHub webhook configuration

In your source repository (the one that sends events):

1. Go to Settings -> Webhooks -> Add webhook
2. Payload URL: `https://YOUR_DOMAIN/webhook`
3. Content type: `application/json`
4. Secret: same value as `WEBHOOK_SECRET`
5. Choose event type: `Just the push event`
6. Save and use `Recent Deliveries` to test

## Networking and firewall

- Keep inbound 443 open for Nginx.
- The webhook receiver container port is localhost-only by default in Compose.
- If using `ufw`, allow HTTPS:

```bash
sudo ufw allow 443/tcp
```

If you do not use Nginx, you can change the Compose port mapping back to a public bind.

## Data persistence

`docker-compose.yml` bind-mounts:

- Host: `./repo`
- Container: `/app/repo`

This means pulled repository data persists across container rebuilds/restarts.

## How updates are processed

- Matching webhook arrives (`push` + `TARGET_REF`)
- Service verifies HMAC signature
- If local repo does not exist: `git clone --branch TARGET_BRANCH`
- Then: `git -C REPO_DIR pull origin TARGET_BRANCH`
- Concurrent events are coalesced into one additional run

## Troubleshooting

- `Invalid signature`: webhook secret in GitHub does not match `WEBHOOK_SECRET`.
- No updates: confirm pushed branch ref equals `TARGET_REF` (for example `refs/heads/gh-pages`).
- Cannot clone/pull: verify `REPO_URL` access from server and repository permissions.
- Wrong upstream port: check `.env` (`SERVE_PORT`) and Nginx `proxy_pass`.

Useful commands:

```bash
docker compose logs --tail=200 webhook-receiver
docker compose exec webhook-receiver env | grep -E 'REPO_|TARGET_|SERVE_PORT'
ls -la repo
```

## Updating the service

When you change code or pull latest changes in this repo:

```bash
git pull
docker compose up -d --build
```

## Security notes

- Do not commit `.env`.
- Rotate `WEBHOOK_SECRET` if exposed.
- Limit exposed ports and consider allowing only GitHub webhook source IPs at firewall/reverse proxy level.
