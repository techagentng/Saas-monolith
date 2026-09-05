package handler

import (
	"errors"
	"log"
	"net/http"
	"net/url"
	"time"

	apperrors "github.com/techagentng/saas-monolith/internal/errors"
	"github.com/techagentng/saas-monolith/internal/identity/service"
)

// AuthErrorQueryParameter is how a failed Google sign-in reaches the browser
// application: the callback redirects to the login page with this parameter
// set to a stable error code. A code, never a message — the frontend owns the
// wording, exactly as it already does for every JSON error this API returns.
const AuthErrorQueryParameter = "auth_error"

// loginPath is where a failed sign-in lands. It is a constant rather than
// configuration because it is this API's own contract with the frontend's
// route table, not a deployment detail.
const loginPath = "/login"

// GoogleAuthHandler owns the two redirect endpoints of the Google OAuth flow.
//
// Everything of value stays server-side. The client secret never leaves this
// process, the authorization code is exchanged here, and the browser is handed
// exactly one thing at the end: the same HttpOnly refresh cookie a password
// login produces. No token — Google's or this application's — is ever placed
// in a URL, a response body, or a log line.
type GoogleAuthHandler struct {
	service       service.GoogleAuthenticationService
	refreshCookie RefreshCookieConfig
	state         OAuthStateConfig
	frontendURL   string
	// enabled is false when the deployment has no Google credentials. The
	// routes are still registered so the frontend gets a coherent answer
	// instead of a 404 it cannot interpret.
	enabled bool
}

func NewGoogleAuthHandler(
	googleService service.GoogleAuthenticationService,
	refreshCookie RefreshCookieConfig,
	state OAuthStateConfig,
	frontendURL string,
	enabled bool,
) *GoogleAuthHandler {
	return &GoogleAuthHandler{
		service:       googleService,
		refreshCookie: refreshCookie,
		state:         state,
		frontendURL:   frontendURL,
		enabled:       enabled,
	}
}

// Start begins the flow: mint state, bind it to this browser, and hand the
// browser to Google.
//
// Nothing the caller sends is trusted beyond an optional return path, which is
// reduced to an internal route before it is stored. In particular there is no
// user id, email, role, or tenant id in this request's contract — who the
// caller turns out to be is decided by Google and the callback, never by
// anything the frontend claims here.
func (h *GoogleAuthHandler) Start(writer http.ResponseWriter, request *http.Request) {
	if !h.enabled {
		_ = apperrors.Map(apperrors.New(apperrors.CodeServiceUnavailable, "google sign-in is not configured", nil)).WriteJSON(writer)
		return
	}
	state, err := NewOAuthState(h.state.Lifetime, time.Now())
	if err != nil {
		log.Printf("google oauth: generating state failed: %v", err)
		_ = apperrors.Map(apperrors.New(apperrors.CodeServiceUnavailable, "google sign-in is unavailable", err)).WriteJSON(writer)
		return
	}
	h.state.setState(writer, state)
	h.state.setReturnPath(writer, SafeRelativePath(request.URL.Query().Get("return_to")))
	// Cache-Control matters here: an intermediary caching this 302 would serve
	// one browser's state value to another, defeating the binding entirely.
	writer.Header().Set("Cache-Control", "no-store")
	http.Redirect(writer, request, h.service.AuthorizationURL(state), http.StatusFound)
}

// Callback completes the flow.
//
// Order is deliberate and security-relevant: state is validated before the
// authorization response is trusted for anything, and the OAuth cookies are
// cleared on every exit path so a callback URL is single-use.
func (h *GoogleAuthHandler) Callback(writer http.ResponseWriter, request *http.Request) {
	if !h.enabled {
		_ = apperrors.Map(apperrors.New(apperrors.CodeServiceUnavailable, "google sign-in is not configured", nil)).WriteJSON(writer)
		return
	}
	query := request.URL.Query()
	returnPath := SafeRelativePath(readCookie(request, OAuthReturnCookieName))
	// Cleared first, so it happens regardless of which branch below returns.
	h.state.clear(writer)
	writer.Header().Set("Cache-Control", "no-store")

	// Google reports a refused or failed consent here. This is checked before
	// the state comparison only so a cancelling user gets "you cancelled"
	// rather than "invalid state"; nothing is trusted from the response yet,
	// and no code is exchanged.
	if googleError := query.Get("error"); googleError != "" {
		h.redirectWithError(writer, request, errorCodeForGoogleError(googleError))
		return
	}
	if err := ValidateOAuthState(readCookie(request, OAuthStateCookieName), query.Get("state"), time.Now()); err != nil {
		h.fail(writer, request, err)
		return
	}

	result, err := h.service.Authenticate(request.Context(), query.Get("code"))
	if err != nil {
		h.fail(writer, request, err)
		return
	}

	// The one and only credential handed to the browser, through the existing
	// cookie infrastructure — same name, same flags, same Path as password
	// login. The frontend then bootstraps through its normal refresh call and
	// receives an ordinary Ed25519 access token; there is no Google-specific
	// session, and no token in this redirect.
	h.refreshCookie.set(writer, result.RefreshToken)
	http.Redirect(writer, request, FrontendRedirect(h.frontendURL, returnPath), http.StatusSeeOther)
}

// fail logs the application error code and redirects the browser to the login
// page carrying that code.
//
// What is logged is the code and nothing else. An authorization code, an ID
// token, or Google's raw error text would all be sensitive, and none of them
// is needed to diagnose which check rejected the sign-in.
func (h *GoogleAuthHandler) fail(writer http.ResponseWriter, request *http.Request, err error) {
	code := apperrors.CodeInternalError
	var appErr *apperrors.AppError
	if errors.As(err, &appErr) {
		code = appErr.Code
	}
	log.Printf("google oauth: sign-in rejected code=%s", code)
	h.redirectWithError(writer, request, code)
}

func (h *GoogleAuthHandler) redirectWithError(writer http.ResponseWriter, request *http.Request, code apperrors.ErrorCode) {
	destination := loginPath + "?" + url.Values{AuthErrorQueryParameter: []string{string(code)}}.Encode()
	http.Redirect(writer, request, FrontendRedirect(h.frontendURL, destination), http.StatusSeeOther)
}

// errorCodeForGoogleError translates Google's `error` parameter into one of
// this application's codes. Google's value is never forwarded: it is
// attacker-influenceable text arriving on a query string, and echoing it into
// a redirect the browser then renders would be a reflection vector.
func errorCodeForGoogleError(googleError string) apperrors.ErrorCode {
	if googleError == "access_denied" {
		return apperrors.CodeOAuthDenied
	}
	return apperrors.CodeOAuthExchangeFailed
}
