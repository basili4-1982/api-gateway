# API Gateway

Reverse proxy / API gateway with per-route JWT authentication, role-based access control, and rate limiting.

## Features

- **Path-based routing** to multiple microservices
- **Per-route JWT authentication** — some routes require JWT, others are public
- **Role-based access control** — check user roles from JWT claims per route
- **Per-route rate limiting** — token bucket algorithm per IP
- **Health checks** — automatic target health monitoring with circuit breaker
- **CORS** — enabled in dev mode, disabled in production
- **Claim-to-header mapping** — pass JWT claims as HTTP headers to backends
- **Graceful shutdown** — wait for active requests to complete
- **Structured logging** — console (dev) or JSON (prod) via Zap

## Quick Start

```bash
# Copy config
cp config.local.example.yaml config.local.yaml
# Edit config.local.yaml with your services and JWT secret

# Run
go run ./cmd/ -config config.local.yaml
```

## Configuration

See [`config.local.example.yaml`](config.local.example.yaml) for all options.

### Routes

| Path | Target | Auth | Roles |
|---|---|---|---|
| `POST /api/v1/auth/login` | auth-api | No | — |
| `POST /api/v1/auth/register` | auth-api | No | — |
| `POST /api/v1/auth/refresh` | auth-api | No | — |
| `/api/v1/auth/*` | auth-api | Yes | — |
| `/api/v1/client/*` | client-api | Yes | user, admin |
| `/api/v1/admin/*` | admin-api | Yes | admin |
| `/api/v1/storage/public/*` | storage-api | No | — |
| `/api/v1/storage/*` | storage-api | Yes | — |

## Docker

```bash
docker build -t api-gateway .
docker run -p 8080:8080 \
  -v $(pwd)/config.local.yaml:/etc/proxy/config.yaml \
  api-gateway
```

## Development

```bash
# Install dependencies
go mod download

# Run with local config
go run ./cmd/ -config config.local.yaml

# Lint
make lint
```

## Architecture

```
docs/obsidian/          # Obsidian vault with documentation
├── API Gateway Architecture.md
├── Routing Configuration.md
├── JWT Authentication.md
├── Rate Limiting.md
├── Targets and Microservices.md
└── Deployment.md
```

## License

MIT
