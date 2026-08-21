package service

import (
	"crypto/ed25519"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/google/uuid"
)

type TokenConfig struct {
	AccessLifetime  time.Duration
	SessionLifetime time.Duration
	PrivateKey      ed25519.PrivateKey
	PublicKey       ed25519.PublicKey
}

type TokenClaims struct {
	UserID    string
	SessionID string
	IssuedAt  int64
	ExpiresAt int64
}

type TokenManager struct {
	config TokenConfig
}

func NewTokenManager(config TokenConfig) *TokenManager { return &TokenManager{config: config} }

func (m *TokenManager) Issue(userID, sessionID string) (string, error) {
	now := time.Now()
	claims := jwt.MapClaims{"sub": userID, "sid": sessionID, "iat": now.Unix(), "exp": now.Add(m.config.AccessLifetime).Unix(), "typ": "access"}
	token := jwt.NewWithClaims(jwt.SigningMethodEdDSA, claims)
	return token.SignedString(m.config.PrivateKey)
}

func (m *TokenManager) Verify(value string) (TokenClaims, error) {
	token, err := jwt.Parse(value, func(token *jwt.Token) (interface{}, error) {
		if token.Method != jwt.SigningMethodEdDSA {
			return nil, jwt.ErrTokenSignatureInvalid
		}
		return m.config.PublicKey, nil
	}, jwt.WithValidMethods([]string{jwt.SigningMethodEdDSA.Alg()}))
	if err != nil || !token.Valid {
		return TokenClaims{}, jwt.ErrTokenInvalidClaims
	}
	claims, ok := token.Claims.(jwt.MapClaims)
	if !ok || claims["typ"] != "access" {
		return TokenClaims{}, jwt.ErrTokenInvalidClaims
	}
	userID, ok := claims["sub"].(string)
	sessionID, sessionOK := claims["sid"].(string)
	issued, issuedOK := claims["iat"].(float64)
	expires, expiresOK := claims["exp"].(float64)
	if !ok || !sessionOK || !issuedOK || !expiresOK {
		return TokenClaims{}, jwt.ErrTokenInvalidClaims
	}
	if _, err := uuid.Parse(userID); err != nil {
		return TokenClaims{}, jwt.ErrTokenInvalidClaims
	}
	if _, err := uuid.Parse(sessionID); err != nil {
		return TokenClaims{}, jwt.ErrTokenInvalidClaims
	}
	return TokenClaims{UserID: userID, SessionID: sessionID, IssuedAt: int64(issued), ExpiresAt: int64(expires)}, nil
}
