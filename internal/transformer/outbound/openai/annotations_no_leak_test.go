package openai

import (
	"context"
	"encoding/json"
	"io"
	"strings"
	"testing"

	"github.com/bestruirui/octopus/internal/transformer/model"
)

// annotations / search_sources 是响应侧字段。客户端回放历史 assistant 轮次时
// 若把它们发给上游，严格的上游会以未知字段拒绝整轮请求。
func TestAnnotationsDoNotLeakIntoOutboundRequest(t *testing.T) {
	maxTokens := int64(16)
	req := &model.InternalLLMRequest{
		Model:     "gpt-4o",
		MaxTokens: &maxTokens,
		Messages: []model.Message{
			{Role: "user", Content: model.MessageContent{Content: strPtr("hi")}},
			{
				Role:    "assistant",
				Content: model.MessageContent{Content: strPtr("answer")},
				Annotations: []model.Annotation{{
					Type:        "url_citation",
					URLCitation: &model.URLCitation{URL: "https://a.com", Title: "A"},
				}},
				SearchSources: []model.SearchSource{{URL: "https://a.com", Title: "A"}},
			},
		},
	}
	out := &ChatOutbound{}
	httpReq, err := out.TransformRequest(context.Background(), req, "https://api.openai.com/v1", "k")
	if err != nil {
		t.Fatalf("outbound: %v", err)
	}
	body, err := io.ReadAll(httpReq.Body)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	t.Logf("outbound body: %s", body)
	for _, forbidden := range []string{"annotations", "search_sources", "url_citation"} {
		if strings.Contains(string(body), forbidden) {
			t.Fatalf("response-only field %q leaked into the upstream request: %s", forbidden, body)
		}
	}
	var probe map[string]any
	if err := json.Unmarshal(body, &probe); err != nil {
		t.Fatalf("body not valid json: %v", err)
	}
}

func strPtr(s string) *string { return &s }
