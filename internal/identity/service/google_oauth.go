package service

import (
	"context"
	"fmt"

	"github.com/coreos/go-oidc/v3/oidc"
	"golang.org/x/oauth2"
	"golang.org/x/oauth2/google"

	apperrors "github.com/techagentng/saas-monolith/internal/errors"
	"github.com/techagentng/saas-monolith/internal/identity/model"
)

// GoogleScopes is the complete set of scopes this application requests. It is
// authentication only: no Gmail, Drive, Calendar or Contacts scope appears
// here, and none may be added without a real Google API integration to
// justify it. "openid" is what makes Google return an ID token at all.
var GoogleScopes = []string{oidc.ScopeOpenID, "email", "profile"}

// googleIssuers are the two issuer strings Google has historically minted ID
// tokens with. Both are legitimate; a verifier pinned to only one would reject
// otherwise-valid tokens, so a verifier exists for each and a token must
// satisfy one of them. This is an allow-list, not a relaxation — an unknown
// issuer still fails.
var googleIssuers = []string{"https://accounts.google.com", "accounts.google.com"}

// googleCertsURL is Google's published JWKS endpoint. Keys are fetched lazily
// on first verification and cached/rotated by go-oidc, so constructing the
// verifier performs no network I/O and cannot make application startup depend
// on Google being reachable.
const googleCertsURL = "https://www.googleapis.com/oauth2/v3/certs"

// GoogleAuthorizationClient owns the halves of the OAuth 2.0 dance that talk
// to Google over the network. It exists as an interface purely so tests can
// drive the callback end to end without a live Google — see the fakes in
// google_authentication_service_test.go.
type GoogleAuthorizationClient interface {
	// AuthorizationURL builds the URL the browser is redirected to. state is
	// opaque here; binding it to the browser is the transport layer's job.
	AuthorizationURL(state string) string
	// ExchangeIDToken trades an authorization code for the raw ID token.
	// Deliberately returns only the ID token: the access and refresh tokens
	// Google also returns are dropped on the floor, because this feature
	// authenticates and never calls a Google API on the user's behalf.
	ExchangeIDToken(ctx context.Context, code string) (string, error)
}

// GoogleIDTokenVerifier turns a raw ID token into a trusted identity. An
// implementation MUST validate signature, issuer, audience and expiry; a
// value of model.GoogleIdentity is meaningless otherwise.
type GoogleIDTokenVerifier interface {
	Verify(ctx context.Context, rawIDToken string) (model.GoogleIdentity, error)
}

// GoogleOAuthConfig carries the deployment's Google credentials. The secret
// lives only here, in the server process; nothing in this package writes it to
// a log, a response body, or a redirect URL.
type GoogleOAuthConfig struct {
	ClientID     string
	ClientSecret string
	RedirectURL  string
}

type googleAuthorizationClient struct{ config *oauth2.Config }

// NewGoogleAuthorizationClient wires golang.org/x/oauth2 against Google's
// well-known endpoints. The OAuth/OIDC cryptography is entirely the
// libraries' — nothing here hand-rolls a token request or a signature check.
func NewGoogleAuthorizationClient(config GoogleOAuthConfig) GoogleAuthorizationClient {
	return &googleAuthorizationClient{config: &oauth2.Config{
		ClientID:     config.ClientID,
		ClientSecret: config.ClientSecret,
		RedirectURL:  config.RedirectURL,
		Endpoint:     google.Endpoint,
		Scopes:       GoogleScopes,
	}}
}

// AuthorizationURL requests an online-only grant and no offline access: a
// refresh token from Google would be a long-lived credential this application
// has no use for and would then have to protect.
//
// prompt=select_account lets someone signed into several Google accounts pick
// deliberately rather than being silently signed in as whichever account the
// browser happened to hold.
func (c *googleAuthorizationClient) AuthorizationURL(state string) string {
	return c.config.AuthCodeURL(state,
		oauth2.AccessTypeOnline,
		oauth2.SetAuthURLParam("prompt", "select_account"),
	)
}

func (c *googleAuthorizationClient) ExchangeIDToken(ctx context.Context, code string) (string, error) {
	token, err := c.config.Exchange(ctx, code)
	if err != nil {
		// Google's own error text can quote the authorization code back at us,
		// so it is never wrapped into the returned error. The generic cause is
		// enough to diagnose from; the code itself is single-use and already
		// spent by the time this fails.
		return "", apperrors.New(apperrors.CodeOAuthExchangeFailed, "google authorization code exchange failed", nil)
	}
	rawIDToken, ok := token.Extra("id_token").(string)
	if !ok || rawIDToken == "" {
		return "", apperrors.New(apperrors.CodeOAuthExchangeFailed, "google token response carried no id_token", nil)
	}
	return rawIDToken, nil
}

type googleIDTokenVerifier struct{ verifiers []*oidc.IDTokenVerifier }

// NewGoogleIDTokenVerifier builds an OIDC verifier bound to this deployment's
// client ID as the expected audience. Audience, expiry, issuer and signature
// are all checked by go-oidc against Google's live JWKS — this package
// implements no JWT verification of its own.
func NewGoogleIDTokenVerifier(ctx context.Context, clientID string) GoogleIDTokenVerifier {
	keySet := oidc.NewRemoteKeySet(ctx, googleCertsURL)
	verifiers := make([]*oidc.IDTokenVerifier, 0, len(googleIssuers))
	for _, issuer := range googleIssuers {
		verifiers = append(verifiers, oidc.NewVerifier(issuer, keySet, &oidc.Config{ClientID: clientID}))
	}
	return &googleIDTokenVerifier{verifiers: verifiers}
}

func (v *googleIDTokenVerifier) Verify(ctx context.Context, rawIDToken string) (model.GoogleIdentity, error) {
	var lastErr error
	for _, verifier := range v.verifiers {
		token, err := verifier.Verify(ctx, rawIDToken)
		if err != nil {
			lastErr = err
			continue
		}
		var claims struct {
			Email         string `json:"email"`
			EmailVerified bool   `json:"email_verified"`
		}
		if err := token.Claims(&claims); err != nil {
			return model.GoogleIdentity{}, apperrors.New(apperrors.CodeOAuthInvalidIdentityToken, "google id token claims are unreadable", err)
		}
		if token.Subject == "" {
			return model.GoogleIdentity{}, apperrors.New(apperrors.CodeOAuthInvalidIdentityToken, "google id token carried no subject", nil)
		}
		return model.GoogleIdentity{Subject: token.Subject, Email: claims.Email, EmailVerified: claims.EmailVerified}, nil
	}
	// Wrapped rather than discarded: the cause names which check failed
	// (signature, audience, expiry) in server logs. It contains no token
	// material — go-oidc reports the failed check, not the token.
	return model.GoogleIdentity{}, apperrors.New(apperrors.CodeOAuthInvalidIdentityToken, fmt.Sprintf("google id token verification failed: %v", lastErr), nil)
}
