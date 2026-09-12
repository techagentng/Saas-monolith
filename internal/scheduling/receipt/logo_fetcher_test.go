package receipt

import (
	"context"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
)

func TestHTTPLogoFetcherFetchesFromTheAllowedHost(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write(onePixelPNGBytes)
	}))
	defer server.Close()

	parsed, err := url.Parse(server.URL)
	if err != nil {
		t.Fatal(err)
	}
	fetcher := NewHTTPLogoFetcher(parsed.Hostname())

	data, contentType, err := fetcher.Fetch(context.Background(), server.URL+"/logo.png")
	if err != nil {
		t.Fatalf("Fetch() error = %v", err)
	}
	if len(data) == 0 {
		t.Fatal("Fetch() returned no bytes")
	}
	if contentType != "image/png" {
		t.Fatalf("content type = %q, want image/png (sniffed, not header-declared)", contentType)
	}
}

func TestHTTPLogoFetcherRejectsADisallowedHost(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Fatal("the disallowed-host request must never reach the server")
	}))
	defer server.Close()

	fetcher := NewHTTPLogoFetcher("some-other-host.example.test")

	if _, _, err := fetcher.Fetch(context.Background(), server.URL+"/logo.png"); err == nil {
		t.Fatal("Fetch() accepted a URL whose host does not match the configured allow-list")
	}
}

func TestHTTPLogoFetcherRejectsANonHTTPScheme(t *testing.T) {
	fetcher := NewHTTPLogoFetcher("media.example.test")

	for _, scheme := range []string{"file", "ftp", "gopher"} {
		if _, _, err := fetcher.Fetch(context.Background(), scheme+"://media.example.test/logo.png"); err == nil {
			t.Fatalf("Fetch() accepted scheme %q", scheme)
		}
	}
}

func TestHTTPLogoFetcherFailsClosedWithNoAllowedHostConfigured(t *testing.T) {
	fetcher := NewHTTPLogoFetcher("")
	if _, _, err := fetcher.Fetch(context.Background(), "https://anything.example.test/logo.png"); err == nil {
		t.Fatal("Fetch() with no configured allow-list must always fail")
	}
}

func TestHTTPLogoFetcherRejectsANonOKStatus(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
	}))
	defer server.Close()
	parsed, _ := url.Parse(server.URL)
	fetcher := NewHTTPLogoFetcher(parsed.Hostname())

	if _, _, err := fetcher.Fetch(context.Background(), server.URL+"/missing.png"); err == nil {
		t.Fatal("Fetch() accepted a 404 response")
	}
}

func TestHTTPLogoFetcherRejectsAnOversizedResponse(t *testing.T) {
	oversized := strings.Repeat("x", int(maxLogoBytes)+1)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(oversized))
	}))
	defer server.Close()
	parsed, _ := url.Parse(server.URL)
	fetcher := NewHTTPLogoFetcher(parsed.Hostname())

	if _, _, err := fetcher.Fetch(context.Background(), server.URL+"/huge.png"); err == nil {
		t.Fatal("Fetch() accepted a response over maxLogoBytes")
	}
}

// onePixelPNGBytes reuses encodeOnePixelPNG from pdf_generator_test.go (same
// package, same _test.go compilation unit).
var onePixelPNGBytes = encodeOnePixelPNG()
