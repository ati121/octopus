package handlers

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
)

func TestDecisionsHandlerUsesDecisionsRequestValidation(t *testing.T) {
	gin.SetMode(gin.TestMode)
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(
		http.MethodPost,
		"/v1/decisions",
		strings.NewReader(`{"model":"gpt-6-luna","questions":[{"type":"choice","name":"department"}]}`),
	)

	decisionsRoute(c)

	if recorder.Code != http.StatusBadRequest {
		t.Fatalf("expected invalid decisions request to return 400, got %d body=%s", recorder.Code, recorder.Body.String())
	}
	if !strings.Contains(recorder.Body.String(), "input is required") {
		t.Fatalf("expected decisions validation error, got %s", recorder.Body.String())
	}
}
