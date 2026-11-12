package main

import (
	"errors"
	"io"
	"log/slog"
	"mime"
	"net/http"
	"path"
	"strconv"
	"strings"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/aws/smithy-go"
	smithyhttp "github.com/aws/smithy-go/transport/http"
)

type gateway struct {
	client *s3.Client
	cfg    settings
	logger *slog.Logger
}

func (g *gateway) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	// Useful for health checks without accessing S3.
	if r.URL.Path == "/_health" {
		w.Header().Set("Cache-Control", "no-store")
		if r.Method != http.MethodGet && r.Method != http.MethodHead {
			w.Header().Set("Allow", "GET, HEAD")
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		w.WriteHeader(http.StatusOK)
		if r.Method == http.MethodGet {
			_, _ = io.WriteString(w, "ok\n")
		}
		return
	}
	if r.Method != http.MethodGet && r.Method != http.MethodHead {
		w.Header().Set("Allow", "GET, HEAD")
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	site, key, err := resolveObjectKey(r.Host, r.URL.Path, g.cfg.baseDomain)
	if err != nil {
		code := http.StatusBadRequest
		if errors.Is(err, errInvalidHost) {
			code = http.StatusNotFound
		}
		http.Error(w, http.StatusText(code), code)
		return
	}

	// Read-only gateway: no bucket listing, PUT, or object writing.
	status, err := g.sendObject(w, r, key)
	if status == http.StatusNotFound && g.cfg.spaFallback && key != site+"/index.html" && mayUseSPAFallback(r.URL.Path) {
		status, err = g.sendObject(w, r, site+"/index.html")
	}
	if status != 0 {
		if status >= 500 {
			g.logger.Error("S3 request failed", "host", r.Host, "key", key, "status", status, "error", err)
		}
		if status == http.StatusNotModified {
			w.WriteHeader(status)
			return
		}
		if status == http.StatusPreconditionFailed || status == http.StatusRequestedRangeNotSatisfiable {
			http.Error(w, http.StatusText(status), status)
			return
		}
		if status == http.StatusNotFound {
			http.NotFound(w, r)
			return
		}
		http.Error(w, "upstream object storage unavailable", http.StatusBadGateway)
	}
}

// sendObject returns status=0 once the response has been handled; otherwise
// the status is returned without touching ResponseWriter (allowing SPA fallback).
func (g *gateway) sendObject(w http.ResponseWriter, r *http.Request, key string) (int, error) {
	ctx := r.Context()
	if r.Method == http.MethodHead {
		input := &s3.HeadObjectInput{Bucket: aws.String(g.cfg.bucket), Key: aws.String(key)}
		applyHeadConditions(input, r)
		result, err := g.client.HeadObject(ctx, input)
		if err != nil {
			return classifyStorageError(err), err
		}
		writeHeaders(w.Header(), key, result.ContentType, result.ContentEncoding, result.ContentLanguage,
			result.CacheControl, result.ETag, result.LastModified, result.ContentLength, g.cfg.cacheControl)
		w.WriteHeader(http.StatusOK)
		return 0, nil
	}

	input := &s3.GetObjectInput{Bucket: aws.String(g.cfg.bucket), Key: aws.String(key)}
	applyGetConditions(input, r)
	if v := r.Header.Get("Range"); v != "" && strings.HasPrefix(v, "bytes=") {
		input.Range = aws.String(v)
	}
	result, err := g.client.GetObject(ctx, input)
	if err != nil {
		return classifyStorageError(err), err
	}
	defer result.Body.Close()

	writeHeaders(w.Header(), key, result.ContentType, result.ContentEncoding, result.ContentLanguage,
		result.CacheControl, result.ETag, result.LastModified, result.ContentLength, g.cfg.cacheControl)
	code := http.StatusOK
	if result.ContentRange != nil {
		w.Header().Set("Content-Range", *result.ContentRange)
		code = http.StatusPartialContent
	}
	w.Header().Set("Accept-Ranges", "bytes")
	w.WriteHeader(code)
	// Streaming responses: avoid buffering entire files in memory.
	if _, err := io.Copy(w, result.Body); err != nil {
		g.logger.Warn("S3 stream interrupted", "key", key, "error", err)
	}
	return 0, nil
}

func applyGetConditions(in *s3.GetObjectInput, r *http.Request) {
	if tag := r.Header.Get("If-None-Match"); tag != "" {
		in.IfNoneMatch = aws.String(tag)
	} else if v := r.Header.Get("If-Modified-Since"); v != "" {
		if t, err := http.ParseTime(v); err == nil {
			in.IfModifiedSince = aws.Time(t)
		}
	}
}

func applyHeadConditions(in *s3.HeadObjectInput, r *http.Request) {
	if tag := r.Header.Get("If-None-Match"); tag != "" {
		in.IfNoneMatch = aws.String(tag)
	} else if v := r.Header.Get("If-Modified-Since"); v != "" {
		if t, err := http.ParseTime(v); err == nil {
			in.IfModifiedSince = aws.Time(t)
		}
	}
}

func writeHeaders(h http.Header, key string, contentType, contentEncoding, contentLanguage, cacheControl, etag *string, lastModified *time.Time, contentLength *int64, defaultCacheControl string) {
	if contentType != nil && *contentType != "" && *contentType != "application/octet-stream" {
		h.Set("Content-Type", *contentType)
	} else if guess := mime.TypeByExtension(path.Ext(key)); guess != "" {
		h.Set("Content-Type", guess)
	} else {
		h.Set("Content-Type", "application/octet-stream")
	}
	if contentEncoding != nil {
		h.Set("Content-Encoding", *contentEncoding)
	}
	if contentLanguage != nil {
		h.Set("Content-Language", *contentLanguage)
	}
	if cacheControl != nil && *cacheControl != "" {
		h.Set("Cache-Control", *cacheControl)
	} else {
		h.Set("Cache-Control", defaultCacheControl)
	}
	if etag != nil {
		h.Set("ETag", *etag)
	}
	if lastModified != nil {
		h.Set("Last-Modified", lastModified.UTC().Format(http.TimeFormat))
	}
	if contentLength != nil && *contentLength >= 0 {
		h.Set("Content-Length", strconvFormatInt(*contentLength))
	}
}

// Kept separate from SDK shapes so routing and headers can be tested in isolation.
func strconvFormatInt(n int64) string { return strconv.FormatInt(n, 10) }

func classifyStorageError(err error) int {
	var apiErr smithy.APIError
	if errors.As(err, &apiErr) {
		switch apiErr.ErrorCode() {
		case "NoSuchKey", "NotFound", "NoSuchVersion", "404":
			return http.StatusNotFound
		case "NotModified", "304":
			return http.StatusNotModified
		case "PreconditionFailed", "412":
			return http.StatusPreconditionFailed
		case "InvalidRange", "416":
			return http.StatusRequestedRangeNotSatisfiable
		}
	}
	// HTTP status is more reliable than a parsed error code on HEAD and 304s.
	var responseErr *smithyhttp.ResponseError
	if errors.As(err, &responseErr) {
		switch responseErr.HTTPStatusCode() {
		case http.StatusNotFound:
			return http.StatusNotFound
		case http.StatusNotModified:
			return http.StatusNotModified
		case http.StatusPreconditionFailed:
			return http.StatusPreconditionFailed
		case http.StatusRequestedRangeNotSatisfiable:
			return http.StatusRequestedRangeNotSatisfiable
		}
	}
	// S3 sometimes returns 403 for a missing key when ListBucket is denied;
	// intentionally do not convert permission failures into false 404s.
	return http.StatusBadGateway
}
