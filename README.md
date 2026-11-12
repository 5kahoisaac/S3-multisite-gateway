
# S3 Multisite Gateway

A lightweight, Docker-based static website gateway that serves multiple websites from a single S3-compatible bucket using dynamic subdomain routing.

Built with **Go, Caddy, and the AWS SDK for Go v2**.

The gateway supports:

- Dynamic hostname-to-S3-prefix routing
- Multiple websites in a single bucket
- AWS S3, Cloudflare R2, MinIO, and compatible storage
- Private S3 buckets using authenticated API requests
- Streaming object delivery
- Optional in-memory LRU caching
- Configurable cache TTL and memory limits
- HTTP caching headers
- Health check endpoint
- Optional SPA fallback
- Docker Compose and Coolify deployment
- Non-root, read-only gateway container
- No per-website configuration or Docker rebuild required

## 1. Architecture

```text
                  Internet
                     |
                     v
             DNS / Wildcard DNS
                     |
                     v
               Coolify Proxy
                 (HTTPS)
                     |
                     v
                   Caddy
                     |
                     v
               Go S3 Gateway
                     |
              Hostname Routing
                     |
              In-Memory Cache
                  /      \
                HIT      MISS
                 |         |
                 |         v
                 |     S3 API
                 |         |
                 |     S3 Bucket
                 |       static
                 |      /  |  \
                 |   test1 test2 test3
                 |         |
                 +---------+
                     |
                     v
                HTTP Response
```

Caddy handles internal HTTP reverse proxying.

The Go gateway determines the S3 object key from the incoming hostname and request path.

No bucket listing or local file synchronization is required.

## 2. Dynamic Subdomain Routing

The gateway maps each single-level subdomain to a corresponding directory prefix within the configured S3 bucket.

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

### Routing examples

Assuming:

```dotenv
BASE_DOMAIN=example.com
S3_BUCKET=static
```

| Request | S3 Object Key |
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

is served from:

```text
s3://static/test1/assets/app.js
```

### Adding a new website

Upload a new directory to the bucket:

```text
s3://static/test4/index.html
```

The gateway automatically supports:

```text
https://test4.example.com
```

No application rebuild, container restart, or configuration change is needed.

**Important:** DNS and the public reverse proxy must also support the new hostname. For automatic subdomain routing, configure wildcard DNS and a compatible wildcard proxy route.

### Routing limitations

- Only one subdomain label is supported.
- `test1.example.com` is valid.
- `abc.test1.example.com` is not supported.
- The root domain `example.com` is not automatically mapped.
- Arbitrary custom-domain mappings are not implemented.
- Unknown or invalid hostnames return HTTP 404.
- Directory listing is not supported.

## 3. Project Structure

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

### File descriptions

| File | Purpose |
|---|---|
| `Dockerfile` | Multi-stage Go build and runtime image |
| `compose.yaml` | Gateway and Caddy service definitions |
| `Caddyfile` | Optional standalone Caddy configuration |
| `.env.example` | Environment variable template |
| `.dockerignore` | Excludes unnecessary build context files |
| `.gitignore` | Excludes credentials and local files |
| `main.go` | Application initialization and HTTP server |
| `routing.go` | Hostname and S3 object key resolution |
| `s3_handler.go` | S3 API requests and response streaming |
| `cache.go` | Optional in-memory LRU response cache |
| `routing_test.go` | Routing and security tests |

The Coolify-compatible Compose configuration starts Caddy through its command arguments and does not mount `Caddyfile`.

## 4. Requirements

- Docker with Docker Compose
- An S3-compatible object storage service
- A bucket containing static website files
- Credentials with object read permissions
- A domain name and configured DNS
- Coolify, another reverse proxy, or a suitable ingress for public deployments

Supported S3-compatible providers include:

- Amazon S3
- Cloudflare R2
- MinIO
- Other implementations compatible with the AWS S3 API

Provider-specific configuration may be necessary.

## 5. Environment Configuration

Create your environment file:

```bash
cp .env.example .env
```

Example configuration:

```dotenv
# Domain routing
BASE_DOMAIN=example.com

# S3 storage
S3_BUCKET=static
AWS_REGION=us-east-1
S3_ENDPOINT=https://s3.example-storage.com
S3_FORCE_PATH_STYLE=true

# AWS / S3 credentials
AWS_ACCESS_KEY_ID=your-access-key
AWS_SECRET_ACCESS_KEY=your-secret-key

# Application
PORT=8080
SPA_FALLBACK=false

# Internal response cache
CACHE_TTL=6h
CACHE_MAX_MB=128
CACHE_MAX_OBJECT_MB=8

# Browser / HTTP cache
DEFAULT_CACHE_CONTROL=public, max-age=300
```

### Environment variable reference

| Variable | Default | Description |
|---|---|---|
| `BASE_DOMAIN` | `example.com` | Parent domain for hostname routing |
| `S3_BUCKET` | Required | S3 bucket name |
| `AWS_REGION` | `us-east-1` | AWS or S3-compatible region |
| `S3_ENDPOINT` | Empty | Custom S3 API endpoint |
| `S3_FORCE_PATH_STYLE` | `true` | Enable path-style S3 addressing |
| `AWS_ACCESS_KEY_ID` | SDK default | Access key |
| `AWS_SECRET_ACCESS_KEY` | SDK default | Secret key |
| `AWS_SESSION_TOKEN` | Empty | Optional temporary session token |
| `PORT` | `8080` | Gateway listening port |
| `SPA_FALLBACK` | `false` | Enable SPA index fallback |
| `CACHE_TTL` | `0s` | Internal cache TTL; zero disables caching |
| `CACHE_MAX_MB` | `128` | Cache body memory budget in MiB |
| `CACHE_MAX_OBJECT_MB` | `8` | Maximum cached object size in MiB |
| `DEFAULT_CACHE_CONTROL` | `public, max-age=60` | Fallback HTTP caching policy |

Defaults refer to the application implementation. The recommended `.env.example` overrides several defaults.

### Runtime vs. build-time variables

All variables above are **runtime environment variables**.

The Docker image does not need S3 credentials or cache settings during compilation.

```text
Build time:
  Dockerfile -> Compile Go executable

Runtime:
  .env -> Container environment -> Go application
```

Changing environment variables does not require rebuilding the image.

However, the container must be recreated to receive updated environment settings.

Changing Go source code, including `cache.go`, requires rebuilding the image.

### Security

Never commit `.env` to Git.

Do not copy credentials into the Docker image or define them using Dockerfile `ARG` or `ENV`.

The AWS SDK supports its default credential provider chain, including IAM roles where available.

## 6. S3 Configuration

### Amazon S3

Use:

```dotenv
S3_BUCKET=static
AWS_REGION=ap-east-1
S3_ENDPOINT=
S3_FORCE_PATH_STYLE=false
```

Change the region to match your bucket.

### Cloudflare R2

Example:

```dotenv
S3_BUCKET=static
AWS_REGION=auto
S3_ENDPOINT=https://ACCOUNT_ID.r2.cloudflarestorage.com
S3_FORCE_PATH_STYLE=true

AWS_ACCESS_KEY_ID=your-r2-access-key
AWS_SECRET_ACCESS_KEY=your-r2-secret-key
```

Replace `ACCOUNT_ID` with your Cloudflare account ID.

Use an R2 API token with permission to read objects from the configured bucket.

### MinIO

Example:

```dotenv
S3_BUCKET=static
AWS_REGION=us-east-1
S3_ENDPOINT=http://minio:9000
S3_FORCE_PATH_STYLE=true
```

If MinIO runs in a separate Docker container, use an accessible Docker service name or network address.

Do not use `localhost:9000` unless MinIO is running in the same network namespace as the gateway.

### Required permissions

The gateway requires read access to the objects it serves.

For AWS S3, an example IAM policy is:

```json
{
  "Version": "2012-10-17",
  "Statement": [
    {
      "Effect": "Allow",
      "Action": ["s3:GetObject"],
      "Resource": "arn:aws:s3:::static/*"
    }
  ]
}
```

This permits reading objects but not uploading, deleting, or modifying them.

AWS S3 may return HTTP 403 instead of HTTP 404 for nonexistent objects when the caller lacks `s3:ListBucket` permission.

The gateway does not require bucket listing for normal object retrieval. If accurate missing-object responses are important, consider granting the minimum appropriate ListBucket permissions.

## 7. Docker Deployment

### Build the image

```bash
docker compose build
```

### Start the services

```bash
docker compose up -d
```

### Check service status

```bash
docker compose ps
```

### View logs

```bash
docker compose logs -f gateway
```

```bash
docker compose logs -f caddy
```

### Stop the services

```bash
docker compose down
```

### Redeploy after source changes

```bash
docker compose up -d --build
```

### Docker networking

The Compose stack contains two services:

```text
caddy:80
   |
   v
gateway:8080
```

Caddy forwards incoming HTTP requests to the gateway.

The services communicate through Docker's internal network.

No host port publishing is required when Coolify provides the public reverse proxy.

This avoids conflicts such as:

```text
Bind for 0.0.0.0:8080 failed:
port is already allocated
```

## 8. Coolify Deployment

The project supports deployment as a Docker Compose application in Coolify.

### Deployment steps

1. Create a new application in Coolify.
2. Connect the Git repository.
3. Select Docker Compose as the build type.
4. Set the Compose file path to `compose.yaml`.
5. Configure the required environment variables.
6. Configure the public domain on the `caddy` service.
7. Deploy the application.

### Service configuration

| Setting | Value |
|---|---|
| Public-facing service | `caddy` |
| Caddy container port | `80` |
| Gateway container port | `8080` |
| Host port binding | None |
| TLS termination | Coolify reverse proxy |

Example public domain:

```text
https://test1.example.com
```

Configure the domain to forward to port `80` of the Caddy container.

### Wildcard domain routing

For automatic project routing, configure wildcard DNS:

```text
*.example.com -> Your reverse proxy
```

Also configure Coolify's proxy to accept matching wildcard hostnames and terminate HTTPS with an appropriate certificate.

Wildcard DNS alone is not enough: the reverse proxy must also recognize the hostname.

Once wildcard routing is configured, adding new S3 prefixes does not require creating individual gateway routes.

### Avoid conflicting port bindings

Do not use:

```yaml
ports:
  - "8080:80"
```

when Coolify is already handling public ingress and port 8080 may be occupied.

Use internal exposure instead:

```yaml
expose:
  - "80"
```

`expose` does not publish a port to the Docker host.

### Environment variables in Coolify

Configure runtime values under the application's environment settings.

Example:

```dotenv
CACHE_TTL=6h
CACHE_MAX_MB=128
CACHE_MAX_OBJECT_MB=8
DEFAULT_CACHE_CONTROL=public, max-age=300
```

Recreate or redeploy the container after changing these variables.

A simple restart of an existing container does not necessarily reload its environment settings.

## 9. Health Checks

The gateway exposes:

```http
GET /_health
```

Example:

```bash
curl -i https://test1.example.com/_health
```

Expected response:

```http
HTTP/2 200
Cache-Control: no-store

ok
```

The health endpoint:

- Supports GET and HEAD.
- Returns HTTP 200 when the application is responding.
- Does not access S3.
- Bypasses the in-memory response cache.
- Does not depend on an individual website existing.

### Docker Compose health check

The following health check can be configured on the Caddy service:

```yaml
healthcheck:
  test:
    - CMD
    - wget
    - -q
    - -O
    - /dev/null
    - http://127.0.0.1:80/_health
  interval: 30s
  timeout: 5s
  retries: 3
  start_period: 20s
```

It checks the Caddy-to-Go request path.

This is a liveness check, not a complete S3 connectivity check.

A healthy status does not guarantee that the S3 endpoint, credentials, or object permissions are working.

### Verify health status

```bash
docker compose ps
```

For a service with a configured and functioning health check, Docker reports:

```text
Up (healthy)
```

## 10. In-Memory S3 Response Cache

The gateway supports optional in-memory caching to reduce repeated S3 API requests.

Without gateway caching:

```text
Request -> Gateway -> S3 -> Response
Request -> Gateway -> S3 -> Response
Request -> Gateway -> S3 -> Response
```

With gateway caching:

```text
First request:
  Gateway -> CACHE MISS -> S3 -> Response

Second request:
  Gateway -> CACHE HIT -> Response

Third request:
  Gateway -> CACHE HIT -> Response
```

Cached responses avoid S3 API requests until the cache entry expires or is evicted.

### Configuration

```dotenv
CACHE_TTL=6h
CACHE_MAX_MB=128
CACHE_MAX_OBJECT_MB=8
```

| Setting | Description |
|---|---|
| `CACHE_TTL` | Lifetime of a cache entry |
| `CACHE_MAX_MB` | Maximum cached response-body memory |
| `CACHE_MAX_OBJECT_MB` | Maximum eligible object size |

### Enable or disable the cache

Enable caching:

```dotenv
CACHE_TTL=6h
```

Disable caching:

```dotenv
CACHE_TTL=0s
```

Supported durations use Go duration syntax:

```text
30s
5m
1h
6h
12h
24h
```

### Memory limits

Example:

```dotenv
CACHE_MAX_MB=128
CACHE_MAX_OBJECT_MB=8
```

The gateway stores up to approximately 128 MiB of cached response bodies.

An individual cached file cannot exceed 8 MiB.

Actual process memory usage is higher because headers, metadata, runtime allocations, and active requests also consume memory.

Larger files continue streaming directly from S3.

### LRU eviction

The gateway uses a Least Recently Used (LRU) eviction strategy.

When the cache reaches its capacity, older, less recently accessed entries are removed to make space.

Expired entries are removed when accessed.

A cache entry may be evicted before its TTL expires if memory pressure requires it.

### Cache isolation

The cache key includes the hostname and request path.

For example:

```text
test1.example.com/index.html
test2.example.com/index.html
```

These are stored separately.

A response cached for one website is not reused for another hostname.

The current S3 routing implementation does not use query parameters when selecting an object, so query parameters are not included in the cache key.

### Cache eligibility

The gateway caches eligible HTTP 200 GET responses.

A response is eligible when:

- The request is a normal GET request.
- The object is within the configured size limit.
- The response is complete.
- The response has a valid Content-Length.
- The response does not prohibit caching.

The gateway bypasses or excludes caching for:

- Non-GET requests
- Health checks
- Range requests
- Conditional requests
- Requests with Authorization headers
- Requests requesting `no-cache` or `no-store`
- HTTP 404 responses
- HTTP 5xx responses
- Responses with `Set-Cookie`
- Responses marked `private`, `no-cache`, or `no-store`
- Responses with `Vary: *`
- Objects larger than the cache size limit

HEAD requests are not served from the in-memory cache and continue to contact S3.

### Cache headers

The gateway adds an `X-Cache` response header to eligible normal requests.

A cache miss produces:

```http
X-Cache: MISS
```

A cache hit produces:

```http
X-Cache: HIT
```

A cache hit does not require another S3 API call.

### Testing the cache

First request:

```bash
curl -sD - -o /dev/null \
  https://test1.example.com/
```

Expected:

```http
HTTP/2 200
X-Cache: MISS
```

Second request:

```bash
curl -sD - -o /dev/null \
  https://test1.example.com/
```

Expected:

```http
HTTP/2 200
X-Cache: HIT
```

These results assume the response is cacheable and the request actually reaches the gateway.

A CDN or browser cache can prevent subsequent requests from reaching the application.

### Bypassing the cache

Use:

```bash
curl -i \
  -H "Cache-Control: no-cache" \
  https://test1.example.com/
```

The gateway bypasses its cache for this request and retrieves the object from S3.

This does not invalidate an existing cached entry.

### Cache invalidation

The current implementation does not provide an API for invalidating individual cache entries.

The cache is stored in the Go process memory.

Restarting the gateway process clears the cache:

```bash
docker compose restart gateway
```

A new gateway container starts with an empty cache.

### Cache consistency

The gateway TTL controls how long cached content can remain available without revalidation.

Example:

```dotenv
CACHE_TTL=6h
```

If `test1/index.html` is updated in S3, the gateway may continue serving its previously cached version until the TTL expires or the entry is evicted.

For frequently updated websites, consider shorter TTLs or clearing the cache during deployment.

For versioned JavaScript, CSS, and image assets, longer TTLs are generally suitable.

### Important limitations

- The cache is not persisted to disk.
- Cache contents disappear when the gateway restarts.
- Multiple gateway replicas do not share cache state.
- Simultaneous cold requests can trigger multiple S3 requests.
- Files larger than the configured limit are not cached.
- Failed or incomplete responses are not cached.
- Cache TTL is independent of HTTP Cache-Control metadata.
- No per-project cache invalidation endpoint is implemented.
- Cache statistics are not currently exposed through a metrics endpoint.

For high-volume deployments requiring shared caching or centralized invalidation, consider Redis, a dedicated caching proxy, or a CDN.

## 11. HTTP Cache-Control

The gateway supports browser-facing HTTP caching headers.

Example:

```dotenv
DEFAULT_CACHE_CONTROL=public, max-age=300
```

This corresponds to a 5-minute freshness lifetime.

However, this setting only applies when the S3 object does not already specify its own Cache-Control metadata.

Existing S3 object metadata takes precedence.

### Gateway cache vs. browser cache

These are separate mechanisms.

| Feature | Gateway Cache | Browser Cache |
|---|---|---|
| Configuration | `CACHE_TTL` | `Cache-Control` |
| Storage | Go process memory | Browser |
| Reduces gateway-to-S3 requests | Yes | Indirectly |
| Shared across website visitors | Yes, per gateway | No |
| Persists across gateway restart | No | Yes, until expiry or eviction |

Example:

```dotenv
CACHE_TTL=6h
DEFAULT_CACHE_CONTROL=public, max-age=300
```

This means:

- The gateway may retain the object for 6 hours.
- Browsers may treat the response as fresh for 5 minutes.
- After browser freshness expires, a new request may still be served from the gateway memory without accessing S3.

### Recommended cache policies

For frequently updated HTML:

```http
Cache-Control: public, max-age=300
```

For fingerprinted static assets:

```http
Cache-Control: public, max-age=31536000, immutable
```

For private or sensitive content:

```http
Cache-Control: no-store
```

Set appropriate Cache-Control metadata on individual S3 objects when different policies are required.

The gateway's default policy applies uniformly to objects without explicit metadata.

## 12. SPA Fallback

Enable single-page application fallback with:

```dotenv
SPA_FALLBACK=true
```

When a requested extensionless route does not exist, the gateway retries the website's root `index.html`.

Example:

```text
https://test1.example.com/dashboard
```

If this does not exist in S3:

```text
test1/dashboard
```

the gateway attempts:

```text
test1/index.html
```

This is useful for applications using client-side routing, including React, Vue, and similar frameworks.

When disabled:

```dotenv
SPA_FALLBACK=false
```

Missing routes return HTTP 404.

### Directory handling

A request ending in `/` is mapped to an `index.html` object.

Example:

```text
test1.example.com/docs/
```

maps to:

```text
test1/docs/index.html
```

A request to `/docs` without a trailing slash is treated as the literal object key `test1/docs`, unless SPA fallback applies after a missing-object response.

## 13. Request and Response Behavior

### Supported methods

| Method | Supported |
|---|---|
| GET | Yes |
| HEAD | Yes |
| POST | No |
| PUT | No |
| PATCH | No |
| DELETE | No |

Unsupported methods return HTTP 405.

### Streaming

S3 objects are streamed to the client.

Files are not downloaded into the Docker image or stored permanently on disk.

This allows large files to be served without fully buffering them in application memory.

Eligible small responses may additionally be retained in the optional in-memory cache.

### Response headers

When available, the gateway forwards relevant S3 metadata, including:

- Content-Type
- Content-Encoding
- Content-Language
- Content-Length
- Cache-Control
- ETag
- Last-Modified

The gateway also supports ranged GET requests and conditional request headers.

### Error behavior

| Status | Meaning |
|---|---|
| 200 | Successful object response |
| 206 | Partial content |
| 304 | Not modified |
| 400 | Invalid request path |
| 404 | Invalid hostname or missing object |
| 405 | Unsupported method |
| 412 | Precondition failed |
| 416 | Invalid range |
| 502 | S3 upstream error, permissions issue, or storage failure |

A 404 from the public reverse proxy can also indicate that the domain is not routed to the application.

## 14. Troubleshooting

### 404 Page Not Found

First check the health endpoint:

```bash
curl -i https://test1.example.com/_health
```

If it returns HTTP 404, check the Coolify domain configuration and routing to the `caddy` service.

If it returns HTTP 200, the proxy-to-gateway path is working.

Next verify:

```dotenv
BASE_DOMAIN=example.com
S3_BUCKET=static
```

And confirm this object exists:

```text
s3://static/test1/index.html
```

The hostname must match the configured base domain.

### Test the gateway directly

From the Caddy container:

```bash
docker compose exec caddy \
  wget -S -O - \
  --header='Host: test1.example.com' \
  http://gateway:8080/
```

This bypasses the public Coolify proxy and Caddy's reverse proxy handler.

If the request succeeds, investigate the external domain and proxy configuration.

### 502 Bad Gateway

Check gateway logs:

```bash
docker compose logs --tail=100 gateway
```

Potential causes:

- Invalid S3 endpoint
- Invalid credentials
- Incorrect bucket name
- Incorrect region
- Missing read permissions
- S3 service unavailable
- Network or TLS errors

### Caddy container cannot start

If Docker reports:

```text
error mounting .../Caddyfile
not a directory
```

Check for an outdated bind mount.

The Coolify-compatible Compose configuration uses Caddy command arguments and does not require mounting `Caddyfile`.

### Port already allocated

If Docker reports:

```text
Bind for 0.0.0.0:8080 failed:
port is already allocated
```

Remove the host `ports` binding.

Use Docker-internal networking and let Coolify handle public ingress.

### Cache always returns MISS

Verify:

```dotenv
CACHE_TTL=6h
```

Also check:

1. The container was recreated after the environment change.
2. `cache.go` is included in the deployed application build.
3. The requested file is smaller than `CACHE_MAX_OBJECT_MB`.
4. The response is HTTP 200 with a valid Content-Length.
5. The response is not marked `private` or `no-store`.
6. The request does not contain cache-bypass or conditional headers.
7. Multiple requests are reaching the same gateway instance.
8. Another caching layer is not serving the response first.

### Cache serves outdated content

The internal TTL may not have expired.

Restart the gateway to clear the cache:

```bash
docker compose restart gateway
```

Alternatively, reduce `CACHE_TTL` for future requests.

## 15. Testing

### Routing tests

Run the isolated routing tests:

```bash
go test routing.go routing_test.go
```

### Full test suite

```bash
go test ./...
```

### Build verification

```bash
go build ./...
```

### Docker image build

```bash
docker compose build
```

### Health check

```bash
curl -i https://test1.example.com/_health
```

### Object retrieval

```bash
curl -i https://test1.example.com/
```

### Cache verification

```bash
curl -sD - -o /dev/null https://test1.example.com/
curl -sD - -o /dev/null https://test1.example.com/
```

Look for `X-Cache: MISS` followed by `X-Cache: HIT` when the gateway cache is enabled and the response is eligible.

## 16. Production Recommendations

For small to medium static websites, the recommended settings are:

```dotenv
BASE_DOMAIN=example.com
S3_BUCKET=static

SPA_FALLBACK=false

CACHE_TTL=6h
CACHE_MAX_MB=128
CACHE_MAX_OBJECT_MB=8

DEFAULT_CACHE_CONTROL=public, max-age=300
```

Additional recommendations:

1. Keep S3 buckets private and use read-only credentials.
2. Terminate HTTPS at Coolify or another trusted reverse proxy.
3. Configure wildcard DNS and routing for automatic site creation.
4. Put a CDN in front of the gateway for high traffic.
5. Use versioned filenames for assets with long cache lifetimes.
6. Set shorter TTLs for frequently updated HTML pages.
7. Monitor gateway memory usage and S3 API request counts.
8. Avoid embedding S3 credentials in Docker images.
9. Keep the application and dependencies updated.
10. Test cache invalidation behavior before production deployments.

## 17. Future Improvements

Potential enhancements include:

- Persistent disk-backed caching
- Redis-based shared caching
- Request coalescing to reduce concurrent S3 cache misses
- Per-project cache TTLs
- Cache purge API
- Cache statistics and Prometheus metrics
- S3 availability readiness checks
- Custom-domain-to-prefix mappings
- Configurable default index documents
- Automatic CDN invalidation
- Cache prewarming after deployments
- Object revalidation using ETag

## License

See the repository's license file, if provided.
