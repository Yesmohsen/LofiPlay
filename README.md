# LofiPlay

LofiPlay is a lightweight 24/7 lofi radio website built with Go and static HTML/CSS/JS.

## Features

- Continuous MP3 streaming from `audio`
- Rotating backgrounds from `backgrounds`
- Current track display
- Online user count over Server-Sent Events
- Basic visit counter
- Docker and Caddy deployment support

## Local Development

```sh
go test ./...
go run .
```

Then open `http://localhost:6001`.

## Media Files

Place audio files in:

```text
audio
```

Place background images in:

```text
backgrounds
```

Supported background extensions are configured in `static/app.js`.

## Configuration

The server can be configured with environment variables:

| Variable | Default | Description |
| --- | --- | --- |
| `LOFIPLAY_ADDR` | `:6001` | HTTP listen address |
| `LOFIPLAY_STATIC_DIR` | `./static` | Static asset root |
| `LOFIPLAY_AUDIO_DIR` | `./audio` | Audio directory |
| `LOFIPLAY_BACKGROUNDS_DIR` | `./backgrounds` | Background image directory |
| `LOFIPLAY_MAX_SSE_PER_IP` | `5` | Max SSE connections per IP |
| `LOFIPLAY_VISITS_FILE` | `data/visits.json` | Persistent total visit counter file |

## Docker Deployment

Build the image:

```sh
docker build -t lofiplay .
```

Run with Compose:

```sh
docker compose up -d
```

The Compose file expects media on the host at:

```text
/root/lofiplay/audio
/root/lofiplay/backgrounds
/root/lofiplay/data
```

You can change the host directory by creating `.env` next to `docker-compose.yml`:

```sh
cp .env.example .env
```

Then edit `LOFIPLAY_HOST_DIR`.

## Moving Servers

The Docker image does not include music or backgrounds. To move to another server, copy these files:

```text
docker-compose.yml
Caddyfile
.env
```

Also copy your host data folder:

```text
$LOFIPLAY_HOST_DIR/audio
$LOFIPLAY_HOST_DIR/backgrounds
$LOFIPLAY_HOST_DIR/data
```

On the new server, run:

```sh
docker compose up -d
```

## Checks

```sh
go test ./...
go vet ./...
```

## Notes

- Total visit counts are persisted to `LOFIPLAY_VISITS_FILE` every 24 hours and during graceful shutdown; the duplicate-IP window remains in memory.
- MP3 duration and seek sync are estimated from MP3 frame data, so VBR files may not sync perfectly.
- Keep large media assets out of git. The Docker build excludes bundled media and expects mounted media in production.
