package app

import "net/http"

// corsMiddleware allows the configured frontend origin(s) to call this API
// directly from the browser. Auth tokens travel in the JSON body and the
// Authorization header, never in a cookie, so no credentialed CORS mode
// (Access-Control-Allow-Credentials) is needed. An origin not present in
// allowedOrigins gets no CORS headers at all — the request still reaches the
// handler for non-browser clients (curl, server-to-server), but a browser
// will block the response, which is the desired default-deny behavior.
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
		}
		if request.Method == http.MethodOptions && request.Header.Get("Access-Control-Request-Method") != "" {
			writer.Header().Set("Access-Control-Allow-Methods", "GET, POST, PATCH, DELETE, OPTIONS")
			writer.Header().Set("Access-Control-Allow-Headers", "Content-Type, Authorization")
			writer.Header().Set("Access-Control-Max-Age", "600")
			writer.WriteHeader(http.StatusNoContent)
			return
		}
		next.ServeHTTP(writer, request)
	})
}
