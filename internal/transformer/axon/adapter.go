// Package axon connects Octopus routing and observation to AxonHub transformers.
// All wire-format parsing and conversion is performed by the pinned llm module.
package axon

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"sync"

	"github.com/bestruirui/octopus/internal/transformer/model"
	"github.com/bestruirui/octopus/internal/utils/iolimit"
	"github.com/looplj/axonhub/llm"
	"github.com/looplj/axonhub/llm/auth"
	"github.com/looplj/axonhub/llm/httpclient"
	"github.com/looplj/axonhub/llm/oauth"
	streamusage "github.com/looplj/axonhub/llm/pipeline/stream"
	"github.com/looplj/axonhub/llm/transformer"
	"github.com/looplj/axonhub/llm/transformer/anthropic"
	"github.com/looplj/axonhub/llm/transformer/doubao"
	"github.com/looplj/axonhub/llm/transformer/gemini"
	"github.com/looplj/axonhub/llm/transformer/jina"
	"github.com/looplj/axonhub/llm/transformer/openai"
	"github.com/looplj/axonhub/llm/transformer/openai/codex"
	"github.com/looplj/axonhub/llm/transformer/openai/decisions"
	"github.com/looplj/axonhub/llm/transformer/openai/responses"
	"github.com/looplj/axonhub/llm/transformer/typesafe"
	"github.com/tidwall/sjson"
)

type Inbound struct {
	Transformer transformer.Inbound
	Format      model.APIFormat
	mu          sync.Mutex
	response    *model.InternalLLMResponse
	collect     func(context.Context) (*model.InternalLLMResponse, error)
}

func NewInbound(kind int) *Inbound {
	i := &Inbound{}
	switch kind {
	case 0:
		i.Transformer, i.Format = openai.NewInboundTransformer(), model.APIFormatOpenAIChatCompletion
	case 1:
		i.Transformer, i.Format = responses.NewInboundTransformer(), model.APIFormatOpenAIResponse
	case 2:
		i.Transformer, i.Format = anthropic.NewInboundTransformer(), model.APIFormatAnthropicMessage
	case 3:
		i.Transformer, i.Format = openai.NewEmbeddingInboundTransformer(), model.APIFormatOpenAIEmbedding
	case 4:
		i.Transformer, i.Format = jina.NewRerankInboundTransformer(), model.APIFormatRerank
	case 5:
		i.Transformer, i.Format = typesafe.NewSystemOneInboundTransformer(), model.APIFormatSystemOne
	case 6:
		i.Transformer, i.Format = decisions.NewInboundTransformer(), model.APIFormatOpenAIDecisions
	default:
		return nil
	}
	return i
}

func (i *Inbound) TransformRequest(ctx context.Context, body []byte) (*model.InternalLLMRequest, error) {
	return i.TransformHTTPRequest(ctx, &http.Request{Header: http.Header{"Content-Type": {"application/json"}}}, body)
}
func (i *Inbound) TransformHTTPRequest(ctx context.Context, req *http.Request, body []byte) (*model.InternalLLMRequest, error) {
	headers := req.Header.Clone()
	if headers == nil {
		headers = make(http.Header)
	}
	if headers.Get("Content-Type") == "" {
		headers.Set("Content-Type", "application/json")
	}
	wire := &httpclient.Request{Method: http.MethodPost, Body: body, ContentType: headers.Get("Content-Type"), Headers: headers}
	if req.URL != nil {
		wire.URL = req.URL.String()
		wire.Path = req.URL.Path
		wire.Query = req.URL.Query()
	}
	r, err := i.Transformer.TransformRequest(ctx, wire)
	if err != nil {
		return nil, err
	}
	r.RawRequest = wire
	return ProjectRequest(r, i.Format, body)
}

func (i *Inbound) SetCollector(collect func(context.Context) (*model.InternalLLMResponse, error)) {
	i.mu.Lock()
	defer i.mu.Unlock()
	i.collect = collect
}

func (o *Outbound) Configure(base, key string) error { return o.configure(base, key) }

// ProjectRequest attaches relay state without serializing the protocol object.
func ProjectRequest(r *llm.Request, format model.APIFormat, raw []byte) (*model.InternalLLMRequest, error) {
	r.APIFormat = format
	view := &model.InternalLLMRequest{Request: *r, RawBody: raw}
	if r.RawRequest != nil {
		view.Query = r.RawRequest.Query
	}
	if format == model.APIFormatOpenAIResponse {
		var input struct {
			Input json.RawMessage `json:"input"`
		}
		if err := json.Unmarshal(raw, &input); err != nil {
			return nil, err
		}
		if len(input.Input) > 0 && input.Input[0] == '[' {
			view.RawInputItems = input.Input
		}
	}
	return view, nil
}

func NativeRequest(ctx context.Context, view *model.InternalLLMRequest) (*llm.Request, error) {
	if view == nil {
		return nil, errors.New("request is nil")
	}
	r := cloneNativeRequest(&view.Request)
	if view.Query != nil {
		if r.RawRequest == nil {
			r.RawRequest = &httpclient.Request{Headers: make(http.Header)}
		}
		r.RawRequest.Query = cloneRequestQuery(view.Query)
	}
	if view.IsOpenAIExactReplayRequest() {
		body, err := sjson.SetRawBytes(view.RawBody, "input", view.OpenAIRawInputItems())
		if err != nil {
			return nil, err
		}
		body, _ = sjson.DeleteBytes(body, "previous_response_id")
		body, _ = sjson.DeleteBytes(body, "conversation")
		body, _ = sjson.SetBytes(body, "model", view.Model)
		return responses.NewInboundTransformer().TransformRequest(ctx, &httpclient.Request{Method: "POST", Body: body, ContentType: "application/json", Headers: make(http.Header), Query: view.Query})
	}
	return r, nil
}

func ProjectResponse(r *llm.Response) (*model.InternalLLMResponse, error) {
	if r == nil {
		return nil, nil
	}
	return &model.InternalLLMResponse{Response: *r}, nil
}

func (i *Inbound) TransformResponse(ctx context.Context, view *model.InternalLLMResponse) ([]byte, error) {
	r := &view.Response
	wire, err := i.Transformer.TransformResponse(ctx, r)
	if err != nil {
		return nil, err
	}
	i.mu.Lock()
	i.response = view
	i.mu.Unlock()
	return wire.Body, nil
}
func (i *Inbound) GetInternalResponse(ctx context.Context) (*model.InternalLLMResponse, error) {
	i.mu.Lock()
	defer i.mu.Unlock()
	if i.collect != nil {
		return i.collect(ctx)
	}
	return i.response, nil
}

type Outbound struct {
	Kind        int
	Transformer transformer.Outbound
	Request     *httpclient.Request
	// EndpointPath overrides the protocol default path (for example /systemone
	// becomes /decisions). Empty keeps the AxonHub default.
	EndpointPath string
	// Transport selects the upstream transport. Empty or "http" uses ordinary
	// HTTP; "websocket" routes Responses requests through AxonHub's WebSocket
	// executor, which owns pooling and continuation.
	Transport string
	// WebSocketProxy carries the channel's proxy settings into the pooled
	// WebSocket dialer.
	WebSocketProxy *httpclient.ProxyConfig
}

const (
	// TransportHTTP is the default upstream transport.
	TransportHTTP = "http"
	// TransportWebSocket routes Responses requests over a pooled upstream
	// WebSocket handled by AxonHub.
	TransportWebSocket = "websocket"
)

func NewOutbound(kind int) *Outbound { return &Outbound{Kind: kind} }

// NewOutboundWithEndpointPath builds an outbound adapter that overrides the
// protocol default path. AxonHub resolves BaseURL + EndpointPath verbatim, so a
// non-empty path also disables its "/v1" normalization.
func NewOutboundWithEndpointPath(kind int, endpointPath string) *Outbound {
	return &Outbound{Kind: kind, EndpointPath: strings.TrimSpace(endpointPath)}
}

// NewWebSocketOutbound builds a Responses outbound that speaks to the upstream
// over WebSocket instead of HTTP/SSE.
func NewWebSocketOutbound(endpointPath string, proxy *httpclient.ProxyConfig) *Outbound {
	return &Outbound{
		Kind:           1,
		EndpointPath:   strings.TrimSpace(endpointPath),
		Transport:      TransportWebSocket,
		WebSocketProxy: proxy,
	}
}

// responsesTransport maps Octopus's transport choice onto AxonHub's constant.
func (o *Outbound) responsesTransport() string {
	if o.Transport == TransportWebSocket {
		return responses.TransportWebSocket
	}
	return responses.TransportHTTP
}

type staticToken string

func (s staticToken) Get(context.Context) (*oauth.OAuthCredentials, error) {
	return &oauth.OAuthCredentials{AccessToken: string(s)}, nil
}

// configure builds the AxonHub outbound transformer. The legacy constructors are
// kept for an empty path so existing channels behave exactly as before; a custom
// path goes through the WithConfig entry points that expose EndpointPath.
func (o *Outbound) configure(base, key string) error {
	var err error
	path := o.EndpointPath
	if path == "" {
		switch o.Kind {
		case 0, 5:
			o.Transformer, err = openai.NewOutboundTransformer(base, key)
		case 1:
			o.Transformer, err = responses.NewOutboundTransformerWithConfig(&responses.Config{
				BaseURL:        base,
				APIKeyProvider: auth.NewStaticKeyProvider(key),
				Transport:      o.responsesTransport(),
			})
		case 2:
			o.Transformer, err = anthropic.NewOutboundTransformer(base, key)
		case 3:
			o.Transformer, err = gemini.NewOutboundTransformer(base, key)
		case 4:
			o.Transformer, err = doubao.NewOutboundTransformer(base, key)
		case 6:
			o.Transformer, err = codex.NewOutboundTransformer(codex.Params{BaseURL: base, TokenProvider: staticToken(key), Transport: "http"})
		case 7:
			o.Transformer, err = jina.NewOutboundTransformer(base, key)
		case 8:
			o.Transformer, err = typesafe.NewOutboundTransformer(base, key)
		case 9:
			o.Transformer, err = decisions.NewOutboundTransformer(base, key)
		default:
			err = fmt.Errorf("unknown outbound type %d", o.Kind)
		}
		return err
	}
	switch o.Kind {
	case 0, 5:
		o.Transformer, err = openai.NewOutboundTransformerWithConfig(&openai.Config{
			PlatformType:   openai.PlatformOpenAI,
			BaseURL:        base,
			APIKeyProvider: auth.NewStaticKeyProvider(key),
			EndpointPath:   path,
		})
	case 1:
		o.Transformer, err = responses.NewOutboundTransformerWithConfig(&responses.Config{
			BaseURL:        base,
			APIKeyProvider: auth.NewStaticKeyProvider(key),
			EndpointPath:   path,
			Transport:      o.responsesTransport(),
		})
	case 2:
		o.Transformer, err = anthropic.NewOutboundTransformerWithConfig(&anthropic.Config{
			Type:           anthropic.PlatformDirect,
			BaseURL:        base,
			APIKeyProvider: auth.NewStaticKeyProvider(key),
			EndpointPath:   path,
		})
	case 3:
		o.Transformer, err = gemini.NewOutboundTransformerWithConfig(gemini.Config{
			BaseURL:        base,
			APIKeyProvider: auth.NewStaticKeyProvider(key),
			EndpointPath:   path,
		})
	case 4:
		o.Transformer, err = doubao.NewOutboundTransformerWithConfig(&doubao.Config{
			BaseURL:        base,
			APIKeyProvider: auth.NewStaticKeyProvider(key),
			EndpointPath:   path,
		})
	case 6:
		o.Transformer, err = codex.NewOutboundTransformer(codex.Params{BaseURL: base, TokenProvider: staticToken(key), Transport: "http", EndpointPath: path})
	case 7:
		o.Transformer, err = jina.NewOutboundTransformerWithConfig(&jina.Config{
			BaseURL:        base,
			APIKeyProvider: auth.NewStaticKeyProvider(key),
			EndpointPath:   path,
		})
	case 8:
		o.Transformer, err = typesafe.NewOutboundTransformerWithConfig(&typesafe.Config{
			BaseURL:        base,
			APIKeyProvider: auth.NewStaticKeyProvider(key),
			EndpointPath:   path,
		})
	case 9:
		o.Transformer, err = decisions.NewOutboundTransformerWithConfig(&decisions.Config{
			BaseURL:        base,
			APIKeyProvider: auth.NewStaticKeyProvider(key),
			EndpointPath:   path,
		})
	default:
		err = fmt.Errorf("unknown outbound type %d", o.Kind)
	}
	return err
}

func (o *Outbound) TransformRequest(ctx context.Context, view *model.InternalLLMRequest, base, key string) (*http.Request, error) {
	wire, err := o.WireRequest(ctx, view, base, key)
	if err != nil {
		return nil, err
	}
	req, err := HTTPRequest(ctx, wire)
	if err != nil {
		return nil, err
	}
	return req, nil
}

// WireRequest builds the AxonHub transport request without converting it into a
// net/http request. The WebSocket transport consumes this shape directly, so
// both transports share one request-construction path.
func (o *Outbound) WireRequest(ctx context.Context, view *model.InternalLLMRequest, base, key string) (*httpclient.Request, error) {
	if err := o.configure(base, key); err != nil {
		return nil, err
	}
	r, err := NativeRequest(ctx, view)
	if err != nil {
		return nil, err
	}
	r, err = streamusage.EnsureUsage().OnInboundLlmRequest(ctx, r)
	if err != nil {
		return nil, err
	}
	o.Request, err = o.Transformer.TransformRequest(ctx, r)
	if err != nil {
		return nil, err
	}
	if finalizer, ok := o.Transformer.(interface {
		FinalizeTransportRequest(*httpclient.Request) *httpclient.Request
	}); ok {
		o.Request = finalizer.FinalizeTransportRequest(o.Request)
	}
	o.Request = httpclient.MergeInboundRequest(o.Request, r.RawRequest)
	return o.Request, nil
}

func HTTPRequest(ctx context.Context, r *httpclient.Request) (*http.Request, error) {
	return httpclient.BuildHttpRequest(ctx, r)
}

func (o *Outbound) TransformResponse(ctx context.Context, r *http.Response) (*model.InternalLLMResponse, error) {
	if o.Transformer == nil {
		if err := o.configure("https://unused.invalid/v1", "unused"); err != nil {
			return nil, err
		}
	}
	b, err := iolimit.ReadAll(r.Body, iolimit.UpstreamResponseMaxBytes())
	if err != nil {
		return nil, err
	}
	if o.Request == nil {
		o.Request = &httpclient.Request{APIFormat: string(o.Transformer.APIFormat()), RequestType: "chat"}
	}
	if strings.Contains(strings.ToLower(r.Header.Get("Content-Type")), "text/event-stream") || (o.Kind == 6 && !json.Valid(b)) {
		return o.aggregateSSE(ctx, b)
	}
	wire := &httpclient.Response{StatusCode: r.StatusCode, Headers: r.Header, Body: b, Request: o.Request}
	native, err := o.Transformer.TransformResponse(ctx, wire)
	if err != nil {
		return nil, err
	}
	view, err := ProjectResponse(native)
	if err != nil {
		return nil, err
	}
	if o.Kind == 1 || o.Kind == 6 {
		var f struct {
			Output json.RawMessage `json:"output"`
		}
		if json.Unmarshal(b, &f) == nil {
			view.RawResponsesOutputItems = f.Output
		}
	}
	return view, nil
}

// ResponsesPayload is used by the existing Octopus WebSocket session transport.
func ResponsesPayload(ctx context.Context, view *model.InternalLLMRequest) ([]byte, error) {
	r, err := NativeRequest(ctx, view)
	if err != nil {
		return nil, err
	}
	t, err := responses.NewOutboundTransformer("https://unused.invalid/v1", "unused")
	if err != nil {
		return nil, err
	}
	wire, err := t.TransformRequest(ctx, r)
	if err != nil {
		return nil, err
	}
	return wire.Body, nil
}

func MarshalResponsesInputItems(messages []model.Message) (json.RawMessage, error) {
	r, err := NativeRequest(context.Background(), &model.InternalLLMRequest{Request: llm.Request{Model: "session", Messages: messages}})
	if err != nil {
		return nil, err
	}
	array := true
	r.TransformOptions.ArrayInputs = &array
	body, err := ResponsesPayload(context.Background(), &model.InternalLLMRequest{Request: *r})
	if err != nil {
		return nil, err
	}
	var wire struct {
		Input json.RawMessage `json:"input"`
	}
	err = json.Unmarshal(body, &wire)
	return wire.Input, err
}
