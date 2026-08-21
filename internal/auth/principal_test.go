package auth

import (
	"context"
	"testing"
)

func TestPrincipalRoundTripsThroughContext(t *testing.T) {
	principal := Principal{UserID: "user", SessionID: "session"}
	got, ok := FromContext(WithPrincipal(context.Background(), principal))
	if !ok || got != principal {
		t.Fatalf("principal = %#v, ok=%v", got, ok)
	}
}
