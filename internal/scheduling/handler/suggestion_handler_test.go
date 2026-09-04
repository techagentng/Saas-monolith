package handler

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	apperrors "github.com/techagentng/saas-monolith/internal/errors"
	"github.com/techagentng/saas-monolith/internal/scheduling/service"
	"github.com/techagentng/saas-monolith/internal/scheduling/suggestions"
)

type fakeSuggestionService struct {
	tenantID string
	list     []suggestions.Suggestion
	err      error
}

func (f *fakeSuggestionService) List(_ context.Context, tenantID string) ([]suggestions.Suggestion, error) {
	f.tenantID = tenantID
	return f.list, f.err
}

func TestSuggestionListShapesTheResponse(t *testing.T) {
	fake := &fakeSuggestionService{list: []suggestions.Suggestion{
		{Category: "Pedicures", Name: "Spa Pedicure", Description: "Extended soak and massage.", SuggestedDurationMinutes: 60},
	}}
	handler := NewSuggestionHandler(fake)
	recorder := httptest.NewRecorder()

	handler.List(recorder, httptest.NewRequest(http.MethodGet, "/", nil), handlerTenantID)

	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s, want 200", recorder.Code, recorder.Body.String())
	}
	if fake.tenantID != handlerTenantID {
		t.Fatalf("tenant passed through = %q, want %q", fake.tenantID, handlerTenantID)
	}

	var result []map[string]any
	if err := json.Unmarshal(recorder.Body.Bytes(), &result); err != nil {
		t.Fatalf("response is not a JSON array: %v", err)
	}
	if len(result) != 1 {
		t.Fatalf("returned %d suggestions, want 1", len(result))
	}
	for _, key := range []string{"category", "name", "description", "suggested_duration_minutes"} {
		if _, ok := result[0][key]; !ok {
			t.Fatalf("suggestion missing %q: %s", key, recorder.Body.String())
		}
	}
}

// The explicit SC1 contract requirement: the suggestion JSON must carry no
// price field, under any name — a suggestion is a template an owner adjusts
// and prices themselves when they create the real service. This asserts
// against the raw JSON object keys, not just the Go struct, so it would catch
// a stray field added directly to the handler's encoding as well as one added
// to the Suggestion type.
func TestSuggestionJSONContainsNoPriceField(t *testing.T) {
	fake := &fakeSuggestionService{list: []suggestions.Suggestion{
		{Category: "Pedicures", Name: "Spa Pedicure", Description: "Extended soak and massage.", SuggestedDurationMinutes: 60},
	}}
	handler := NewSuggestionHandler(fake)
	recorder := httptest.NewRecorder()

	handler.List(recorder, httptest.NewRequest(http.MethodGet, "/", nil), handlerTenantID)

	var result []map[string]any
	if err := json.Unmarshal(recorder.Body.Bytes(), &result); err != nil {
		t.Fatalf("response is not a JSON array: %v", err)
	}
	if len(result) != 1 {
		t.Fatalf("returned %d suggestions, want 1", len(result))
	}
	for key := range result[0] {
		lower := strings.ToLower(key)
		if strings.Contains(lower, "price") || strings.Contains(lower, "cost") || strings.Contains(lower, "amount") {
			t.Fatalf("suggestion JSON exposed a price-shaped field %q: %s", key, recorder.Body.String())
		}
	}
	// Exactly the four agreed fields — nothing more, nothing fewer.
	want := map[string]bool{"category": true, "name": true, "description": true, "suggested_duration_minutes": true}
	if len(result[0]) != len(want) {
		t.Fatalf("suggestion has %d fields, want exactly %d: %v", len(result[0]), len(want), result[0])
	}
	for key := range result[0] {
		if !want[key] {
			t.Fatalf("suggestion exposed unexpected field %q, want only %v", key, want)
		}
	}
}

func TestSuggestionListReturnsAnEmptyArrayRatherThanNull(t *testing.T) {
	handler := NewSuggestionHandler(&fakeSuggestionService{list: nil})
	recorder := httptest.NewRecorder()

	handler.List(recorder, httptest.NewRequest(http.MethodGet, "/", nil), handlerTenantID)

	if got := strings.TrimSpace(recorder.Body.String()); got != "[]" {
		t.Fatalf("body = %s, want []", got)
	}
}

func TestSuggestionListMapsServiceErrors(t *testing.T) {
	fake := &fakeSuggestionService{err: apperrors.New(apperrors.CodeInvalidRequest, "invalid tenant id", nil)}
	handler := NewSuggestionHandler(fake)
	recorder := httptest.NewRecorder()

	handler.List(recorder, httptest.NewRequest(http.MethodGet, "/", nil), "not-a-uuid")

	if recorder.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", recorder.Code)
	}
	assertErrorCode(t, recorder, "INVALID_REQUEST")
}

// compile-time guard: the fake must keep satisfying the real interface.
var _ service.SuggestionService = (*fakeSuggestionService)(nil)
