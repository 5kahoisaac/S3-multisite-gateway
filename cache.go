package main

import (
	"container/list"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"
)

type cacheEntry struct {
	key     string
	headers http.Header
	body    []byte
	expires time.Time
}

type responseCache struct {
	mu                                  sync.Mutex
	items                               map[string]*list.Element
	lru                                 *list.List
	ttl                                 time.Duration
	maxBytes, maxObjectBytes, usedBytes int64
}

func cacheEnv(key, fallback string) string {
	v := strings.TrimSpace(os.Getenv(key))
	if v != "" {
		return v
	}
	return fallback
}

func cacheMBEnv(key string, defaultMB int64) (int64, error) {
	raw := cacheEnv(key, strconv.FormatInt(defaultMB, 10))
	n, err := strconv.ParseInt(raw, 10, 64)
	if err != nil || n < 1 || n > 4096 {
		return 0, fmt.Errorf("%s must be an integer from 1 to 4096 (MiB)", key)
	}
	return n * 1024 * 1024, nil
}

func withCache(next http.Handler, logger *slog.Logger) (http.Handler, error) {
	ttl, err := time.ParseDuration(cacheEnv("CACHE_TTL", "0s"))
	if err != nil || ttl < 0 {
		return nil, fmt.Errorf("CACHE_TTL must be a non-negative duration, e.g. 6h")
	}
	if ttl == 0 {
		return next, nil
	}

	maxBytes, err := cacheMBEnv("CACHE_MAX_MB", 128)
	if err != nil {
		return nil, err
	}

	maxObjectBytes, err := cacheMBEnv("CACHE_MAX_OBJECT_MB", 8)
	if err != nil {
		return nil, err
	}
	if maxObjectBytes > maxBytes {
		return nil, fmt.Errorf("CACHE_MAX_OBJECT_MB must not exceed CACHE_MAX_MB")
	}

	c := &responseCache{
		items:          make(map[string]*list.Element),
		lru:            list.New(),
		ttl:            ttl,
		maxBytes:       maxBytes,
		maxObjectBytes: maxObjectBytes,
	}

	logger.Info("S3 response cache enabled",
		"ttl", ttl,
		"max_bytes", maxBytes,
		"max_object_bytes", maxObjectBytes,
	)
	return c.wrap(next), nil
}

func (c *responseCache) get(key string) (cacheEntry, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()

	elem, ok := c.items[key]
	if !ok {
		return cacheEntry{}, false
	}
	entry := elem.Value.(*cacheEntry)

	if !time.Now().Before(entry.expires) {
		c.remove(elem)
		return cacheEntry{}, false
	}

	c.lru.MoveToFront(elem)
	return *entry, true
}

func (c *responseCache) remove(elem *list.Element) {
	entry := elem.Value.(*cacheEntry)
	delete(c.items, entry.key)
	c.lru.Remove(elem)
	c.usedBytes -= int64(len(entry.body))
}

func (c *responseCache) put(entry *cacheEntry) {
	size := int64(len(entry.body))
	if size > c.maxObjectBytes || size > c.maxBytes {
		return
	}

	c.mu.Lock()
	defer c.mu.Unlock()

	if old, ok := c.items[entry.key]; ok {
		c.remove(old)
	}

	for (c.usedBytes+size > c.maxBytes || c.lru.Len() >= 10000) &&
		c.lru.Len() > 0 {
		c.remove(c.lru.Back())
	}

	c.items[entry.key] = c.lru.PushFront(entry)
	c.usedBytes += size
}

type cacheWriter struct {
	http.ResponseWriter
	status         int
	headers        http.Header
	body           []byte
	maxObjectBytes int64
	tooLarge       bool
}

func (w *cacheWriter) WriteHeader(code int) {
	if w.status != 0 {
		return
	}
	w.status = code
	w.headers = w.ResponseWriter.Header().Clone()
	w.ResponseWriter.WriteHeader(code)
}

func (w *cacheWriter) Write(p []byte) (int, error) {
	if w.status == 0 {
		w.WriteHeader(http.StatusOK)
	}

	if !w.tooLarge && w.status == http.StatusOK {
		if int64(len(w.body))+int64(len(p)) <= w.maxObjectBytes {
			w.body = append(w.body, p...)
		} else {
			w.body = nil
			w.tooLarge = true
		}
	}
	return w.ResponseWriter.Write(p)
}

func (w *cacheWriter) Flush() {
	if w.status == 0 {
		w.WriteHeader(http.StatusOK)
	}
	if f, ok := w.ResponseWriter.(http.Flusher); ok {
		f.Flush()
	}
}

func (c *responseCache) wrap(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet ||
			r.URL.Path == "/_health" ||
			r.Header.Get("Range") != "" ||
			r.Header.Get("If-Range") != "" ||
			r.Header.Get("If-None-Match") != "" ||
			r.Header.Get("If-Modified-Since") != "" ||
			r.Header.Get("Authorization") != "" ||
			strings.Contains(strings.ToLower(r.Header.Get("Cache-Control")), "no-cache") ||
			strings.Contains(strings.ToLower(r.Header.Get("Cache-Control")), "no-store") {
			next.ServeHTTP(w, r)
			return
		}

		// Query parameters do not affect S3 key selection in this gateway.
		cacheKey := strings.ToLower(r.Host) + "\x00" + r.URL.EscapedPath()

		if stored, ok := c.get(cacheKey); ok {
			for key, values := range stored.headers {
				for _, value := range values {
					w.Header().Add(key, value)
				}
			}
			w.Header().Set("X-Cache", "HIT")
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write(stored.body)
			return
		}

		w.Header().Set("X-Cache", "MISS")
		writer := &cacheWriter{
			ResponseWriter: w,
			maxObjectBytes: c.maxObjectBytes,
		}

		next.ServeHTTP(writer, r)

		if writer.status != http.StatusOK ||
			writer.tooLarge ||
			writer.headers == nil {
			return
		}

		policy := strings.ToLower(writer.headers.Get("Cache-Control"))

		if strings.Contains(policy, "no-store") ||
			strings.Contains(policy, "no-cache") ||
			strings.Contains(policy, "private") ||
			strings.Contains(policy, "max-age=0") ||
			writer.headers.Get("Set-Cookie") != "" ||
			writer.headers.Get("Vary") == "*" {
			return
		}

		// Never cache an incomplete S3 stream.
		expected, err := strconv.ParseInt(
			writer.headers.Get("Content-Length"), 10, 64,
		)
		if err != nil || expected != int64(len(writer.body)) {
			return
		}

		writer.headers.Del("X-Cache")

		c.put(&cacheEntry{
			key:     cacheKey,
			headers: writer.headers,
			body:    writer.body,
			expires: time.Now().Add(c.ttl),
		})
	})
}