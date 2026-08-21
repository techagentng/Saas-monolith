package auth

import (
	"net/http"
	"strings"
	"time"

	apperrors "github.com/techagentng/saas-monolith/internal/errors"
	"github.com/techagentng/saas-monolith/internal/identity/repository"
	"github.com/techagentng/saas-monolith/internal/identity/service"
)

type Middleware struct {
	Tokens   *service.TokenManager
	Sessions repository.SessionRepository
}

func (m Middleware) Wrap(next http.Handler) http.Handler {
	return http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		parts := strings.Fields(request.Header.Get("Authorization"))
		if len(parts) != 2 || !strings.EqualFold(parts[0], "Bearer") {
			_ = apperrors.Map(apperrors.New(apperrors.CodeInvalidCredentials, "invalid credentials", nil)).WriteJSON(writer)
			return
		}
		claims, err := m.Tokens.Verify(parts[1])
		if err != nil {
			_ = apperrors.Map(apperrors.New(apperrors.CodeInvalidCredentials, "invalid credentials", err)).WriteJSON(writer)
			return
		}
		if _, err := m.Sessions.FindActive(request.Context(), claims.UserID, claims.SessionID, time.Now()); err != nil {
			_ = apperrors.Map(err).WriteJSON(writer)
			return
		}
		next.ServeHTTP(writer, request.WithContext(WithPrincipal(request.Context(), Principal{UserID: claims.UserID, SessionID: claims.SessionID})))
	})
}
