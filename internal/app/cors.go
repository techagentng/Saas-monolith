package app

import "net/http"

// corsMiddleware allows the configured frontend origin(s) to call this API
// directly from the browser. The refresh credential is an HttpOnly cookie, so
// credentialed CORS is required: a browser only sends that cookie
// cross-origin when the response carries Access-Control-Allow-Credentials
// alongside a concrete origin. That is precisely why the allow-list echoes
// the matched origin and never "*" — the wildcard is illegal in credentialed
// mode, and the allow-list is what stops this from becoming an open
// cross-site session endpoint. An origin not present in allowedOrigins gets
// no CORS headers at all — the request still reaches the handler for
// non-browser clients (curl, server-to-server), but a browser will block the
// response, which is the desired default-deny behavior.
func corsMiddleware(allowedOrigins []string, next http.Handler) http.Handler {
	allowed := make(map[string]struct{}, len(allowedOrigins))
	for _, origin := range allowedOrigins {
		allowed[origin] = struct{}{}
	}
	return http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		origin := request.Header.Get("Origin")
		writer.Header().Add("Vary", "Origin")
		if _, ok := allowed[origin]; ok {
			writer.Header().Set("Access-Control-Allow-Origin", origin)
			writer.Header().Set("Access-Control-Allow-Credentials", "true")
		}
		if request.Method == http.MethodOptions && request.Header.Get("Access-Control-Request-Method") != "" {
			// PUT is used by the write-once endpoints (tenant currency, staff
			// service capabilities, staff working hours). A browser preflight
			// for any of them fails unless PUT is listed here, so it must stay
			// in sync with the methods the router actually registers.
			writer.Header().Set("Access-Control-Allow-Methods", "GET, POST, PUT, PATCH, DELETE, OPTIONS")
			writer.Header().Set("Access-Control-Allow-Headers", "Content-Type, Authorization")
			writer.Header().Set("Access-Control-Max-Age", "600")
			writer.WriteHeader(http.StatusNoContent)
			return
		}
		next.ServeHTTP(writer, request)
	})
}
