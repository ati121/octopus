package handlers

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
)

func TestSystemOneHandlerUsesSystemOneRequestValidation(t *testing.T) {
	gin.SetMode(gin.TestMode)
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(
		http.MethodPost,
		"/v1/systemone",
		strings.NewReader(`{"model":"jev-latest","questions":{"is_urgent":{"type":"noul","instructions":"Does this convey urgency?"}}}`),
	)

	systemOne(c)

	if recorder.Code != http.StatusBadRequest {
		t.Fatalf("expected invalid system one request to return 400, got %d body=%s", recorder.Code, recorder.Body.String())
	}
	if !strings.Contains(recorder.Body.String(), "state is required") {
		t.Fatalf("expected system one validation error, got %s", recorder.Body.String())
	}
}
