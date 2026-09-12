package receipt

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// maxLogoBytes bounds a fetched logo — generous for a real business logo,
// small enough that a misconfigured or hostile URL cannot be used to exhaust
// memory or bandwidth on this server.
const maxLogoBytes = 5 * 1024 * 1024

// logoFetchTimeout bounds how long one receipt request waits on a slow or
// unresponsive logo host before falling back to name-only rendering.
const logoFetchTimeout = 5 * time.Second

// HTTPLogoFetcher fetches a tenant's logo over HTTP(S), restricted to a
// single allow-listed host — the SSRF defense S12-BE requires. It is NOT a
// general-purpose URL fetcher: a URL whose host does not exactly match
// allowedHost is refused before any network call is made, regardless of
// scheme or path.
//
// As of S12-BE, nothing in the platform stores a tenant logo URL at all
// (confirmed by audit: no such column exists anywhere in the schema), so
// BookingReceiptService never actually populates Data.LogoURL yet and this
// type's real network path is currently unreachable in production. It
// exists — and is tested against an httptest.Server — so a future
// tenant-branding/logo-upload feature has a safe fetch path ready rather
// than needing this SSRF review redone later under time pressure.
type HTTPLogoFetcher struct {
	allowedHost string
	client      *http.Client
}

// NewHTTPLogoFetcher restricts fetches to allowedHost — pass the hostname of
// the app's own configured media origin (e.g. the host component of
// config.MediaPublicBaseURL), matching S12-BE's instruction to restrict the
// source to "known media hosts/storage conventions already used by the app"
// rather than an open allow-list. An empty allowedHost makes every fetch
// fail closed.
func NewHTTPLogoFetcher(allowedHost string) *HTTPLogoFetcher {
	return &HTTPLogoFetcher{
		allowedHost: allowedHost,
		client:      &http.Client{Timeout: logoFetchTimeout},
	}
}

// Fetch validates rawURL's scheme and host BEFORE making any network call,
// then reads at most maxLogoBytes and sniffs the real content type from the
// bytes actually received — never a client- or server-declared header —
// mirroring internal/scheduling/model's own "sniff the real bytes" rule for
// uploaded service images.
func (f *HTTPLogoFetcher) Fetch(ctx context.Context, rawURL string) ([]byte, string, error) {
	if f.allowedHost == "" {
		return nil, "", errors.New("receipt: no logo host is configured")
	}
	parsed, err := url.Parse(rawURL)
	if err != nil {
		return nil, "", fmt.Errorf("receipt: invalid logo URL: %w", err)
	}
	if parsed.Scheme != "https" && parsed.Scheme != "http" {
		return nil, "", fmt.Errorf("receipt: unsupported logo URL scheme %q", parsed.Scheme)
	}
	if !strings.EqualFold(parsed.Hostname(), f.allowedHost) {
		return nil, "", fmt.Errorf("receipt: logo host %q is not the configured media host", parsed.Hostname())
	}

	request, err := http.NewRequestWithContext(ctx, http.MethodGet, rawURL, nil)
	if err != nil {
		return nil, "", err
	}
	response, err := f.client.Do(request)
	if err != nil {
		return nil, "", err
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return nil, "", fmt.Errorf("receipt: logo host returned status %d", response.StatusCode)
	}

	limited := io.LimitReader(response.Body, maxLogoBytes+1)
	body, err := io.ReadAll(limited)
	if err != nil {
		return nil, "", err
	}
	if int64(len(body)) > maxLogoBytes {
		return nil, "", fmt.Errorf("receipt: logo exceeds %d bytes", maxLogoBytes)
	}

	return body, http.DetectContentType(body), nil
}

// compile-time guard.
var _ LogoFetcher = (*HTTPLogoFetcher)(nil)
