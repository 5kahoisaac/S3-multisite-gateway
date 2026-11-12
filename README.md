# S3 Multisite Gateway

A lightweight, Docker-based static website gateway that serves multiple websites from a single S3-compatible bucket using dynamic subdomain routing.

Built with **Go, Caddy, and AWS SDK for Go v2**.

## Features

- Dynamic subdomain-to-S3-prefix routing
- Multiple websites hosted from a single S3 bucket
- Compatible with AWS S3, Cloudflare R2, MinIO, and other S3-compatible storage
- Private bucket access using S3 credentials
- Streaming object delivery without downloading entire files
- Optional in-memory LRU response caching
- Configurable cache TTL and memory limits
- HTTP Cache-Control headers
- Health check endpoint
- Optional SPA fallback
- Docker and Docker Compose support
- Non-root, read-only gateway container
- No per-website configuration required

## Architecture

```text
                HTTP Request
                     |
                     v
                   Caddy
                     |
                     v
               Go S3 Gateway
                     |
               Hostname Router
                     |
               In-Memory Cache
                  /       \
                 HIT      MISS
                  |         |
                  |         v
                  |      S3 API
                  |         |
                  |      Bucket: static
                  |       /   |   \
                  |    test1 test2 test3
                  |         |
                  +---------+
                     |
                     v
                HTTP Response
```

Caddy handles HTTP reverse proxying. The Go gateway resolves the requested hostname and path into an S3 object key.

S3 objects are retrieved on demand. No bucket listing, synchronization, or local file storage is required.

## Dynamic Subdomain Routing

Each single-level subdomain is mapped to its corresponding prefix inside the configured S3 bucket.

### Example bucket structure

```text
s3://static/
├── test1/
│   ├── index.html
│   ├── about.html
│   └── assets/
│       ├── app.js
│       └── style.css
│
├── test2/
│   ├── index.html
│   └── images/
│       └── logo.png
│
└── test3/
    └── index.html
```

With:

```dotenv
BASE_DOMAIN=example.com
S3_BUCKET=static
```

| Incoming Request | S3 Object Key |
|---|---|
| `test1.example.com/` | `test1/index.html` |
| `test1.example.com/about.html` | `test1/about.html` |
| `test1.example.com/assets/app.js` | `test1/assets/app.js` |
| `test2.example.com/` | `test2/index.html` |
| `test2.example.com/images/logo.png` | `test2/images/logo.png` |
| `test3.example.com/` | `test3/index.html` |

For example:

```text
https://test1.example.com/assets/app.js
```

Retrieves:

```text
s3://static/test1/assets/app.js
```

### Adding new websites

Simply upload a new prefix:

```text
s3://static/test4/index.html
```

The gateway can immediately serve `test4.example.com`, provided DNS and incoming HTTP routing are configured to direct that hostname to the gateway.

No application configuration changes, image rebuilds, or container restarts are required.

### Routing limitations

- Only single-level subdomains are supported.
- `test1.example.com` is valid.
- `abc.test1.example.com` is not supported.
- The root domain is not automatically mapped.
- Arbitrary custom-domain mappings are not supported.
- Invalid hostnames return HTTP 404.
- Directory listing is disabled.

## Project Structure

```text
s3-multisite-gateway/
├── Dockerfile
├── compose.yaml
├── Caddyfile
├── .dockerignore
├── .gitignore
├── .env.example
├── README.md
├── go.mod
├── main.go
├── routing.go
├── routing_test.go
├── s3_handler.go
└── cache.go
```

| File | Description |
|---|---|
| `Dockerfile` | Multi-stage Go build and runtime image |
| `compose.yaml` | Caddy and gateway service definitions |
| `Caddyfile` | Optional standalone Caddy configuration |
| `.env.example` | Environment configuration template |
| `.dockerignore` | Docker build context exclusions |
| `.gitignore` | Git exclusions |
| `main.go` | Application initialization |
| `routing.go` | Hostname and S3 key routing |
| `s3_handler.go` | S3 object retrieval and HTTP response handling |
| `cache.go` | In-memory response caching |
| `routing_test.go` | Routing and security tests |

The standard Compose configuration runs Caddy using command-line arguments and does not require mounting `Caddyfile`.

## Requirements

- Docker and Docker Compose
- S3-compatible object storage
- An existing bucket containing static files
- Credentials permitting object reads
- Appropriate DNS configuration for public hosting

## Environment Variables

Create a local environment file:

```bash
cp .env.example .env
```

Example:

```dotenv
# Domain routing
BASE_DOMAIN=example.com

# S3 storage
S3_BUCKET=static
AWS_REGION=us-east-1
S3_ENDPOINT=https://s3.example-storage.com
S3_FORCE_PATH_STYLE=true

# S3 credentials
AWS_ACCESS_KEY_ID=your-access-key
AWS_SECRET_ACCESS_KEY=your-secret-key

# Application
PORT=8080
SPA_FALLBACK=false

# In-memory cache
CACHE_TTL=6h
CACHE_MAX_MB=128
CACHE_MAX_OBJECT_MB=8

# HTTP caching
DEFAULT_CACHE_CONTROL=public, max-age=300
```

### Configuration reference

| Variable | Default | Description |
|---|---|---|
| `BASE_DOMAIN` | `example.com` | Parent domain |
| `S3_BUCKET` | Required | S3 bucket name |
| `AWS_REGION` | `us-east-1` | Storage region |
| `S3_ENDPOINT` | Empty | Custom S3 endpoint |
| `S3_FORCE_PATH_STYLE` | `true` | Path-style S3 requests |
| `AWS_ACCESS_KEY_ID` | SDK credential chain | S3 access key |
| `AWS_SECRET_ACCESS_KEY` | SDK credential chain | S3 secret |
| `AWS_SESSION_TOKEN` | Empty | Optional session token |
| `PORT` | `8080` | Gateway HTTP port |
| `SPA_FALLBACK` | `false` | Enable SPA routing fallback |
| `CACHE_TTL` | `0s` | Internal cache TTL |
| `CACHE_MAX_MB` | `128` | Cache capacity in MiB |
| `CACHE_MAX_OBJECT_MB` | `8` | Maximum cached object size in MiB |
| `DEFAULT_CACHE_CONTROL` | `public, max-age=60` | Fallback Cache-Control header |

The example configuration overrides some application defaults.

### Runtime vs. build-time

All listed variables are **runtime configuration**.

The Docker image does not require S3 credentials or cache settings during compilation.

Changing runtime variables does not require rebuilding the image, but the container must be recreated to receive updated environment values.

Changes to Go source files require rebuilding the image.

Never copy `.env` into the Docker image or commit credentials to Git.

## S3 Configuration

### AWS S3

```dotenv
S3_BUCKET=static
AWS_REGION=ap-east-1
S3_ENDPOINT=
S3_FORCE_PATH_STYLE=false
```

Use the actual region of your bucket.

### Cloudflare R2

```dotenv
S3_BUCKET=static
AWS_REGION=auto
S3_ENDPOINT=https://ACCOUNT_ID.r