# S3 multisite gateway

Serve any single-level subdomain from its matching directory (object prefix) inside one S3-compatible bucket. No per-project route definitions, bucket listing, or copying objects to the container filesystem are required.

## Routing

| Incoming request | S3 object key (`S3_BUCKET=static`) |
|---|---|
| `http://test1.example.com/` | `test1/index.html` |
| `http://test1.example.com/assets/app.css` | `test1/assets/app.css` |
| `http://test2.example.com/` | `test2/index.html` |
| `http://test3.example.com/docs/` | `test3/docs/index.html` |

S3 URI examples: `s3://static/test1/index.html`, `s3://static/test2/index.html`.
A new `testN/` prefix becomes available immediately (provided DNS for `testN.example.com` resolves to the server).

## Quick start

1. Copy the environment template and set your actual S3 storage configuration:

   ```bash
   cp .env.example .env
   # Edit .env: BASE_DOMAIN, S3_BUCKET, AWS_REGION, S3_ENDPOINT, credentials.
   ```

2. Upload static sites to the configured bucket:

   ```bash
   # Using AWS S3 (omit --endpoint-url for AWS):
   aws s3 sync ./test1/ s3://static/test1/
   aws s3 sync ./test2/ s3://static/test2/

   # For S3-compatible providers instead:
   aws --endpoint-url https://s3.example-storage.com s3 sync ./test1/ s3://static/test1/
   ```

3. Start the stack:

   ```bash
   docker compose up -d --build
   ```

4. Test locally without modifying DNS:

   ```bash
   curl -i -H 'Host: test1.example.com' http://127.0.0.1:8080/
   curl -i -H 'Host: test2.example.com' http://127.0.0.1:8080/
   curl -i -H 'Host: test1.example.com' http://127.0.0.1:8080/assets/app.css
   curl -i http://127.0.0.1:8080/_health
   ```

   In production point a wildcard DNS A/AAAA record (`*.example.com`) at your proxy/server.

## Environment variables

| Name | Required | Description |
|---|---|---|
| `BASE_DOMAIN` | yes | Parent domain, e.g. `example.com`. Only one subdomain label is accepted. |
| `S3_BUCKET` | yes | Bucket name, e.g. `static`. |
| `AWS_REGION` | yes | `us-east-1` for AWS/MinIO, or `auto` for providers such as R2. |
| `S3_ENDPOINT` | no | Custom S3 API URL, blank for AWS itself. |
| `S3_FORCE_PATH_STYLE` | no | `true` for many S3-compatible providers, `false` for AWS S3. |
| `AWS_ACCESS_KEY_ID` | conditional | Key if not using IAM roles or another SDK credential source. |
| `AWS_SECRET_ACCESS_KEY` | conditional | Secret for the key above. |
| `AWS_SESSION_TOKEN` | conditional | Temporary credential session token. |
| `HTTP_PORT` | no | Caddy host HTTP port; default `8080`. |
| `PORT` | no | Internal gateway port; Compose pins it to `8080`. |
| `SPA_FALLBACK` | no | Default `false`. Retry `test1/index.html` for missing extensionless routes. |
| `DEFAULT_CACHE_CONTROL` | no | Default `public, max-age=60` when S3 metadata is absent. |

### How `.env` works

`Dockerfile` creates the application image and **does not contain secrets or load `.env` during build**. `compose.yaml` uses `env_file: .env` to inject environment values **at container start**. This avoids embedding S3 keys in image layers. Compose also reads `.env` to interpolate `HTTP_PORT`.

Without Compose, run directly:

```bash
docker build -t s3-multisite .
docker run --rm -p 8080:8080 --env-file .env s3-multisite
# Test: curl -H 'Host: test1.example.com' http://127.0.0.1:8080/
```

### Git and Docker ignore rules

- `.gitignore` excludes local `.env` credentials, certificates, generated Go binaries, test coverage, logs, caches, and editor/OS files. `.env.example` stays trackable as a safe configuration template.
- `.dockerignore` excludes `.git`, `.gitignore`, GitHub configuration, secrets (including `.env.example`), local artifacts, tests, docs, and Compose/Caddy configuration from the **Go image build context**. These files remain in your project directory; Compose still reads `.env` and mounts `Caddyfile` from the host.
- The Dockerfile needs only Go source files and `go.mod` (plus `go.sum`, if generated) from the build context. Do not use `COPY .env` or set secrets using Docker `ARG`/`ENV` during image builds.

### AWS S3

For AWS use:

```dotenv
S3_ENDPOINT=
S3_FORCE_PATH_STYLE=false
AWS_REGION=ap-east-1
```

The service uses the AWS SDK for Go v2 default credential provider chain. In managed environments prefer an IAM role instead of static access keys. Minimal authorization for reading existing objects: `s3:GetObject` on `arn:aws:s3:::static/*`. To consistently distinguish missing keys (404) from access denied (403), AWS S3 also requires `s3:ListBucket` on the bucket; the application does **not** list bucket contents. Without ListBucket permission, AWS can return 403 for a missing object, which the gateway intentionally reports as an upstream error rather than guessing.

### S3-compatible services

Set `S3_ENDPOINT=https://...`, `S3_FORCE_PATH_STYLE=true` and the appropriate `AWS_REGION`. For MinIO running in another Docker service, e.g. use `S3_ENDPOINT=http://minio:9000`, *not* `localhost:9000` (that would refer to the gateway container itself).

## HTTPS / Caddy vs Nginx

This compose stack intentionally listens on **HTTP port 8080** to work immediately and to sit behind an existing TLS-terminating reverse proxy/CDN. Caddy preserves the `Host` header when proxying; the gateway determines the bucket prefix from that header.

For public `https://*.example.com`, configure **wildcard DNS and a wildcard TLS certificate** (for example using DNS-01 with your DNS provider). Caddy's stock image does not include provider-specific DNS modules. You can terminate HTTPS with Cloudflare, a load balancer, or another proxy; alternatively, build Caddy with the appropriate DNS plugin and configure Caddy's `tls { dns ... }`, or mount a wildcard certificate and change the Caddyfile to a `*.example.com` site with `tls /certs/fullchain.pem /certs/privkey.pem` and expose 443.

Nginx + Lua/OpenResty is possible, but you'd need to maintain AWS Signature V4 request signing, header handling, credentials, retries and API quirks. Here, the official Go SDK handles S3 API authentication, while Caddy deals with HTTP proxying.

## Behavior and limitations

- Only `GET` and `HEAD` are accepted; no uploads or directory listing.
- `/` reads `{site}/index.html`; `/docs/` reads `{site}/docs/index.html`. `/docs` looks up an object literally named `{site}/docs`, unless SPA fallback is enabled.
- Responses stream from S3; files are **not predownloaded** into the Docker image or permanently cached on the gateway. Place a CDN in front for significant request volume or set appropriate per-object `Cache-Control` metadata.
- S3 `Content-Type` (with extension-based fallback when absent or octet-stream), `Content-Encoding`, `Cache-Control`, `ETag`, `Last-Modified` and `Content-Length` are forwarded; the gateway also supports ranged GET and conditional requests.
- A missing object returns 404 when S3 signals `NoSuchKey`/`NotFound`. Credentials, permissions or S3 outages result in a gateway error (502).
- The reserved `/_health` endpoint returns 200 without contacting S3.
- Arbitrary custom domain mappings and nested subdomains are not implemented; a single subdomain label determines the S3 prefix.
- Multiple slash/dot-segment path traversal is rejected or normalized to prevent reading outside a site's prefix.

## Tests

```bash
# Isolated routing/security tests use only Go standard library and can run offline:
go test routing.go routing_test.go

# Full application tests/build download AWS SDK modules:
go test ./...
go build ./...
```
