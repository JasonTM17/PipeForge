package httpapi

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestWriteProblemReturnsStableShape(t *testing.T) {
	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodGet, "/missing", nil)
	WriteProblem(recorder, request, http.StatusNotFound, "NOT_FOUND", "Resource was not found.", nil)

	if recorder.Code != http.StatusNotFound {
		t.Fatalf("unexpected status: %d", recorder.Code)
	}
	var body Problem
	if err := json.Unmarshal(recorder.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode problem: %v", err)
	}
	if body.Code != "NOT_FOUND" || body.Message == "" || body.StatusCode != http.StatusNotFound {
		t.Fatalf("unexpected problem body: %+v", body)
	}
}
