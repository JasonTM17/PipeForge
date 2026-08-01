package httpapi

import (
	"encoding/json"
	"net/http"

	"github.com/JasonTM17/PipeForge/go/internal/observability"
)

type Problem struct {
	StatusCode int            `json:"statusCode"`
	Code       string         `json:"code"`
	Message    string         `json:"message"`
	RequestID  string         `json:"requestId"`
	Details    map[string]any `json:"details,omitempty"`
}

func WriteProblem(w http.ResponseWriter, r *http.Request, status int, code, message string, details map[string]any) {
	problem := Problem{
		StatusCode: status,
		Code:       code,
		Message:    message,
		RequestID:  observability.RequestID(r.Context()),
		Details:    details,
	}
	w.Header().Set("Content-Type", "application/problem+json")
	w.WriteHeader(status)
	if err := json.NewEncoder(w).Encode(problem); err != nil {
		return
	}
}
