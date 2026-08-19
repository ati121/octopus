package handlers

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
)

func TestRerankHandlerUsesRerankRequestValidation(t *testing.T) {
	gin.SetMode(gin.TestMode)
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(
		http.MethodPost,
		"/v1/rerank",
		strings.NewReader(`{"model":"Pro/BAAI/bge-reranker-v2-m3","query":"octopus"}`),
	)

	rerank(c)

	if recorder.Code != http.StatusBadRequest {
		t.Fatalf("expected invalid rerank request to return 400, got %d body=%s", recorder.Code, recorder.Body.String())
	}
	if !strings.Contains(recorder.Body.String(), "documents are required") {
		t.Fatalf("expected rerank validation error, got %s", recorder.Body.String())
	}
}
