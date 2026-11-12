package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/service/s3"
)

type settings struct {
	port, baseDomain, bucket, region, endpoint, cacheControl string
	spaFallback                                              bool
	pathStyle                                                bool
}

func env(key, fallback string) string {
	if v := strings.TrimSpace(os.Getenv(key)); v != "" {
		return v
	}
	return fallback
}

func readSettings() (settings, error) {
	s := settings{
		port:         env("PORT", "8080"),
		baseDomain:   strings.ToLower(strings.TrimSuffix(env("BASE_DOMAIN", "example.com"), ".")),
		bucket:       strings.TrimSpace(os.Getenv("S3_BUCKET")),
		region:       env("AWS_REGION", "us-east-1"),
		endpoint:     strings.TrimSpace(os.Getenv("S3_ENDPOINT")),
		cacheControl: env("DEFAULT_CACHE_CONTROL", "public, max-age=60"),
	}
	if s.bucket == "" {
		return s, errors.New("S3_BUCKET is required")
	}
	if strings.Contains(s.baseDomain, ":") || !strings.Contains(s.baseDomain, ".") || s.baseDomain == "" {
		return s, errors.New("BASE_DOMAIN must be a DNS name such as example.com")
	}
	var err error
	s.spaFallback, err = strconv.ParseBool(env("SPA_FALLBACK", "false"))
	if err != nil {
		return s, fmt.Errorf("SPA_FALLBACK: %w", err)
	}
	s.pathStyle, err = strconv.ParseBool(env("S3_FORCE_PATH_STYLE", "true"))
	if err != nil {
		return s, fmt.Errorf("S3_FORCE_PATH_STYLE: %w", err)
	}
	if _, err := strconv.Atoi(s.port); err != nil {
		return s, fmt.Errorf("invalid PORT: %w", err)
	}
	return s, nil
}

func main() {
	logger := slog.New(slog.NewJSONHandler(os.Stdout, nil))
	s, err := readSettings()
	if err != nil {
		logger.Error("invalid configuration", "error", err)
		os.Exit(1)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	cfg, err := config.LoadDefaultConfig(ctx, config.WithRegion(s.region))
	if err != nil {
		logger.Error("cannot load AWS configuration", "error", err)
		os.Exit(1)
	}
	client := s3.NewFromConfig(cfg, func(o *s3.Options) {
		if s.endpoint != "" {
			o.BaseEndpoint = aws.String(s.endpoint)
		}
		o.UsePathStyle = s.pathStyle
	})
	gateway := &gateway{client: client, cfg: s, logger: logger}
	server := &http.Server{
		Addr:              ":" + s.port,
		Handler:           gateway,
		ReadHeaderTimeout: 10 * time.Second,
		IdleTimeout:       90 * time.Second,
		WriteTimeout:      0, // Streaming large S3 objects may exceed fixed write deadlines.
		MaxHeaderBytes:    1 << 20,
	}
	logger.Info("S3 multisite gateway listening", "port", s.port, "bucket", s.bucket, "base_domain", s.baseDomain)
	if err := server.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
		logger.Error("HTTP server terminated", "error", err)
		os.Exit(1)
	}
}
