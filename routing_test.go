package main

import (
	"errors"
	"testing"
)

func TestResolveObjectKey(t *testing.T) {
	tests := []struct{ host, uri, wantSite, wantKey string }{
		{"test1.example.com", "/", "test1", "test1/index.html"},
		{"test2.example.com", "/assets/site.css", "test2", "test2/assets/site.css"},
		{"ABC.example.com:8080", "/docs/", "abc", "abc/docs/index.html"},
		{"x.example.com", "/docs", "x", "x/docs"},
		{"x.example.com", "/a//b/", "x", "x/a/b/index.html"},
		{"x.example.com.", "/", "x", "x/index.html"},
	}
	for _, tt := range tests {
		site, key, err := resolveObjectKey(tt.host, tt.uri, "example.com")
		if err != nil || site != tt.wantSite || key != tt.wantKey {
			t.Errorf("resolve(%q,%q)=(%q,%q,%v), want (%q,%q,nil)", tt.host, tt.uri, site, key, err, tt.wantSite, tt.wantKey)
		}
	}
}

func TestInvalidHostsAndPaths(t *testing.T) {
	for _, host := range []string{"example.com", "foo.bar.example.com", "-foo.example.com", "foo_.example.com", "foo.other.com", "foo.example.com:bad", "evilfooexample.com"} {
		if _, _, err := resolveObjectKey(host, "/", "example.com"); !errors.Is(err, errInvalidHost) {
			t.Errorf("host %q accepted: %v", host, err)
		}
	}
	for _, uri := range []string{"/../secret", "/a/../../secret", "/./foo", "/foo\\bar", "/a/\x00file", "relative"} {
		if _, _, err := resolveObjectKey("ok.example.com", uri, "example.com"); !errors.Is(err, errInvalidPath) {
			t.Errorf("path %q accepted: %v", uri, err)
		}
	}
}

func TestSPA(t *testing.T) {
	for _, tt := range []struct {
		path string
		want bool
	}{
		{"/", true}, {"/dashboard", true}, {"/dashboard/", true},
		{"/assets/app.js", false}, {"/favicon.ico", false},
	} {
		if got := mayUseSPAFallback(tt.path); got != tt.want {
			t.Errorf("mayUseSPAFallback(%q)=%v, want %v", tt.path, got, tt.want)
		}
	}
}
