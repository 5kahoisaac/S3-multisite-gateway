package main

import (
	"errors"
	"fmt"
	"net"
	"path"
	"strconv"
	"strings"
	"unicode/utf8"
)

var (
	errInvalidHost = errors.New("invalid hostname")
	errInvalidPath = errors.New("invalid request path")
)

// resolveObjectKey maps one-level subdomains to isolated S3 prefixes.
// For example: test1.example.com + /assets/app.js => test1/assets/app.js.
func resolveObjectKey(hostHeader, requestPath, baseDomain string) (site, key string, err error) {
	host := strings.ToLower(strings.TrimSuffix(hostHeader, "."))
	if strings.Contains(host, ":") {
		var splitErr error
		var portString string
		host, portString, splitErr = net.SplitHostPort(host)
		if splitErr != nil {
			return "", "", errInvalidHost
		}
		portNumber, portErr := strconv.Atoi(portString)
		if portErr != nil || portNumber < 1 || portNumber > 65535 {
			return "", "", errInvalidHost
		}
	}

	suffix := "." + strings.ToLower(strings.TrimSuffix(baseDomain, "."))
	if !strings.HasSuffix(host, suffix) {
		return "", "", errInvalidHost
	}
	site = strings.TrimSuffix(host, suffix)
	if !validSiteLabel(site) {
		return "", "", errInvalidHost
	}

	if !strings.HasPrefix(requestPath, "/") || !utf8.ValidString(requestPath) || strings.Contains(requestPath, "\\") {
		return "", "", errInvalidPath
	}
	for _, part := range strings.Split(requestPath, "/") {
		if part == "." || part == ".." || strings.ContainsRune(part, '\x00') {
			return "", "", errInvalidPath
		}
	}

	// S3 keys must stay under {site}/, regardless of client-provided path.
	normalized := strings.TrimPrefix(path.Clean(requestPath), "/")
	if requestPath == "/" || strings.HasSuffix(requestPath, "/") {
		if normalized == "." || normalized == "" {
			normalized = "index.html"
		} else {
			normalized += "/index.html"
		}
	}
	if normalized == "" || normalized == "." {
		normalized = "index.html"
	}
	return site, fmt.Sprintf("%s/%s", site, normalized), nil
}

func validSiteLabel(site string) bool {
	if len(site) == 0 || len(site) > 63 || site[0] == '-' || site[len(site)-1] == '-' {
		return false
	}
	for _, b := range site {
		if (b >= 'a' && b <= 'z') || (b >= '0' && b <= '9') || b == '-' {
			continue
		}
		return false
	}
	return true
}

func mayUseSPAFallback(requestPath string) bool {
	if strings.HasSuffix(requestPath, "/") {
		return true
	}
	return path.Ext(requestPath) == ""
}
