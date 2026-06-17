# LofiPlay 🎵

[![ghcr.io](https://img.shields.io/badge/ghcr.io-yesmohsen/lofiplay-2496ED?logo=docker&logoColor=white)](https://github.com/Yesmohsen/LofiPlay/pkgs/container/lofiplay)
[![Build and Push Docker Image](https://github.com/Yesmohsen/LofiPlay/actions/workflows/docker.yml/badge.svg)](https://github.com/Yesmohsen/LofiPlay/actions/workflows/docker.yml)

**24/7 Lofi Radio** — [lofiplay.ir](https://lofiplay.ir)

A synchronized lofi music radio station. Every listener hears the same track at the same position, no matter where they are in the world. Built with Go (zero external deps) + vanilla JS.

## Features

- 🎵 24/7 synchronized audio streaming
- 🖼️ Hourly rotating background images (same for all users)
- 📊 Live online user counter + total visit counter
- ⌨️ Keyboard shortcuts (M = mute, D = distraction-free)
- 📱 Mobile responsive
- 🐳 Dockerized (~5MB scratch image)

## Tech Stack

- **Backend:** Go 1.23 (stdlib only — no third-party modules)
- **Frontend:** Vanilla HTML/CSS/JS (no frameworks, no build step)
- **Streaming:** Custom MP3 broadcaster with burst buffer for fast join
- **Reverse proxy:** Caddy (automatic HTTPS, zstd/gzip compression)
- **Deploy:** Docker + GitHub Container Registry

## Quick Start

```sh
go run .
```

Then open `http://localhost:6001`.

## Configuration

| Variable | Default | Description |
|---|---|---|
| `LOFIPLAY_ADDR` | `:6001` | HTTP listen address |
| `LOFIPLAY_STATIC_DIR` | `./static` | Static asset root |
| `LOFIPLAY_AUDIO_DIR` | `./audio` | Audio directory |
| `LOFIPLAY_BACKGROUNDS_DIR` | `./backgrounds` | Background image directory |
| `LOFIPLAY_MAX_SSE_PER_IP` | `5` | Max SSE connections per IP |
| `LOFIPLAY_VISITS_FILE` | `data/visits.json` | Persistent total visit counter file |

## Docker

```sh
# Pull and run
docker pull ghcr.io/yesmohsen/lofiplay:latest
docker run -p 6001:6001 -v /path/to/audio:/app/static/audio -v /path/to/bg:/app/static/backgrounds ghcr.io/yesmohsen/lofiplay:latest

# Or with Compose
docker compose up -d
```

The image is automatically built and pushed to GitHub Container Registry on every push to `main`.

## Media Files

Audio and background images are **not** included in the Docker image. Mount them at runtime:

```
/app/static/audio       → MP3 files
/app/static/backgrounds → images (jpg, jpeg, png, webp, gif, avif)
```

## Deployment

The project includes a `docker-compose.yml` ready for [Dockage](https://dockage.co) or any Docker host. For production with a custom domain, pair with Caddy for automatic HTTPS.

## Checks

```sh
go vet ./...
go test ./...
```
