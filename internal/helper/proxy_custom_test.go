package helper

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/bestruirui/octopus/internal/model"
	"github.com/bestruirui/octopus/internal/transformer/outbound"
)

// 用一个本地 HTTP 正向代理桩代替上游：请求只有走了渠道的自定义代理才能拿到模型列表。
func TestFetchModelsUsesChannelCustomProxy(t *testing.T) {
	var proxiedHosts []string
	proxy := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		proxiedHosts = append(proxiedHosts, r.URL.Host)
		if r.URL.Path != "/v1/models" {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"object":"list","data":[{"id":"gpt-4o"}]}`))
	}))
	defer proxy.Close()

	models, err := FetchModels(context.Background(), model.Channel{
		Type:      outbound.OutboundTypeOpenAIChat,
		BaseUrls:  []model.BaseUrl{{URL: "http://upstream.invalid/v1"}},
		Keys:      []model.ChannelKey{{Enabled: true, ChannelKey: "sk-test"}},
		ProxyMode: model.ProxyUsageModeCustom,
		ProxyURL:  proxy.URL,
	})
	if err != nil || len(models) != 1 || models[0] != "gpt-4o" {
		t.Fatalf("models=%v err=%v", models, err)
	}
	if len(proxiedHosts) == 0 || proxiedHosts[0] != "upstream.invalid" {
		t.Fatalf("expected request to go through the custom proxy, got hosts %v", proxiedHosts)
	}
}
