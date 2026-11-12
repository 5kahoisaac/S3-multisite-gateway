## S3 response caching

The gateway supports an optional in-memory LRU cache to reduce repeated
S3 API requests and improve response latency.

Caching is implemented directly in Go without requiring Redis,
additional Docker services, or a custom Caddy build.

### Architecture

```text
Browser
   |
   | Cache-Control: public, max-age=300
   v
Coolify Reverse Proxy
   |
   v
Caddy
   |
   v
Go S3 Gateway
   |
   +-- Cache HIT --> Return cached response
   |
   +-- Cache MISS
         |
         v
       S3 Bucket
         |
         v
       Store in memory (if eligible)
         |
         v
       Return response
```

### Configuration

Add the following variables to `.env`:

```dotenv
# Enable in-memory caching for 6 hours
CACHE_TTL=6h

# Maximum cache storage (MiB)
CACHE_MAX_MB=128

# Maximum size of a single cached object (MiB)
CACHE_MAX_OBJECT_MB=8

# Browser cache duration (5 minutes)
DEFAULT_CACHE_CONTROL=public, max-age=300
```

| Variable | Default | Description |
|---|---|---|
| `CACHE_TTL` | `0s` | Internal cache duration. `0s` disables caching. |
| `CACHE_MAX_MB` | `128` | Maximum memory budget for cached response bodies, in MiB. |
| `CACHE_MAX_OBJECT_MB` | `8` | Maximum individual object size eligible for caching, in MiB. |
| `DEFAULT_CACHE_CONTROL` | `public, max-age=60` | Fallback HTTP Cache-Control when S3 metadata does not specify one. |

The memory limit applies to cached response bodies. Actual process
memory usage will be higher due to metadata, allocations, and active
requests.

### How caching works

On the first request:

1. Resolve the hostname to its S3 prefix.
2. Look up the requested object in the gateway cache.
3. If absent, retrieve the object from S3.
4. Stream the response to the client.
5. Cache successful, eligible responses.
6. Mark the response with `X-Cache: MISS`.

On subsequent requests:

1. Find the matching cached object.
2. Verify the cache entry has not expired.
3. Return the cached response without contacting S3.
4. Mark the response with `X-Cache: HIT`.

Cache entries are isolated by hostname and request path.

For example:

```text
test1.example.com/index.html
  -> static/test1/index.html

test2.example.com/index.html
  -> static/test2/index.html
```

These requests do not share cached content.

### Cache eligibility

The gateway caches:

- Successful HTTP 200 GET responses.
- Objects smaller than `CACHE_MAX_OBJECT_MB`.
- Responses with a valid Content-Length matching the complete body.
- Responses without restrictive cache directives.

The gateway does not cache:

- HTTP 404 or 5xx responses.
- Objects larger than the configured limit.
- Incomplete S3 responses.
- Requests with Range headers.
- Conditional requests such as If-None-Match.
- Requests with Authorization headers.
- Responses marked `private`, `no-store`, or `no-cache`.
- Responses containing Set-Cookie.
- Health check responses.

HEAD requests continue to use S3 directly.

### Test caching

Run:

```bash
curl -sD - -o /dev/null https://test1.example.com/
```

First request:

```http
HTTP/2 200
X-Cache: MISS
```

Repeat:

```bash
curl -sD - -o /dev/null https://test1.example.com/
```

Expected response:

```http
HTTP/2 200
X-Cache: HIT
```

To bypass the gateway cache:

```bash
curl -i \
  -H "Cache-Control: no-cache" \
  https://test1.example.com/
```

This forces a new upstream request but does not immediately
invalidate an existing cached entry.

If a CDN is serving the response, it may prevent the request from
reaching the gateway. Test directly against the gateway to verify
its internal cache behavior.

### Cache eviction

The gateway uses a Least Recently Used (LRU) eviction policy.

When the cache reaches its configured capacity, older,
less recently accessed entries are removed to make room for
new content.

Expired entries are removed when accessed.

### Cache invalidation

The cache is stored in process memory.

It is cleared automatically when the gateway container restarts.

For example:

```bash
docker compose restart gateway
```

This is a simple way to clear the entire cache after an S3 deployment.

Individual object purging is not currently supported.

### Important limitations

- Cache state is not shared between gateway replicas.
- Cache contents disappear when the process restarts.
- Simultaneous cache misses can trigger multiple S3 requests.
- Files exceeding the maximum object size are streamed from S3.
- Content updated in S3 can remain stale until its gateway TTL expires.
- The gateway cache TTL is independent of S3 Cache-Control metadata.
- A 6-hour TTL can therefore retain content longer than its
  browser-facing max-age directive.

For frequently updated HTML, consider a shorter cache TTL or
a deployment-triggered cache purge.

For versioned JavaScript, CSS, images, and fonts, longer TTLs
are generally appropriate.

### Production recommendations

Suggested configuration for small static websites:

```dotenv
CACHE_TTL=6h
CACHE_MAX_MB=128
CACHE_MAX_OBJECT_MB=8
DEFAULT_CACHE_CONTROL=public, max-age=300
```

For high-traffic deployments, consider using Cloudflare or another
CDN in front of the gateway to reduce requests reaching your server.

The in-memory cache is intended for a single gateway instance
or deployments where independent per-instance caches are acceptable.