// Package axon connects Octopus routing and observation to AxonHub transformers.
// All wire-format parsing and conversion is performed by the pinned llm module.
package axon

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"sync"

	"github.com/bestruirui/octopus/internal/providercompat"
	"github.com/bestruirui/octopus/internal/transformer/model"
	"github.com/bestruirui/octopus/internal/utils/iolimit"
	"github.com/looplj/axonhub/llm"
	"github.com/looplj/axonhub/llm/httpclient"
	"github.com/looplj/axonhub/llm/oauth"
	"github.com/looplj/axonhub/llm/transformer"
	"github.com/looplj/axonhub/llm/transformer/anthropic"
	"github.com/looplj/axonhub/llm/transformer/doubao"
	"github.com/looplj/axonhub/llm/transformer/gemini"
	"github.com/looplj/axonhub/llm/transformer/jina"
	"github.com/looplj/axonhub/llm/transformer/openai"
	"github.com/looplj/axonhub/llm/transformer/openai/codex"
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
	return ProjectRequest(r, i.Format, body)
}

// ProjectRequest creates an observation/session view. Native remains authoritative.
func ProjectRequest(r *llm.Request, format model.APIFormat, raw []byte) (*model.InternalLLMRequest, error) {
	b, err := json.Marshal(r)
	if err != nil {
		return nil, err
	}
	var view model.InternalLLMRequest
	if err := json.Unmarshal(b, &view); err != nil {
		return nil, fmt.Errorf("project request: %w", err)
	}
	view.Native, view.RawAPIFormat, view.RawRequest = r, format, raw
	if r.Embedding != nil {
		b, _ := json.Marshal(r.Embedding)
		var fields struct {
			Input *model.EmbeddingInput `json:"input"`
		}
		if err := json.Unmarshal(b, &fields); err != nil {
			return nil, err
		}
		view.EmbeddingInput = fields.Input
	}
	if r.Rerank != nil {
		view.RerankPayload = append([]byte(nil), raw...)
	}
	if r.SystemOne != nil {
		view.SystemOnePayload = append([]byte(nil), raw...)
	}
	if format == model.APIFormatOpenAIResponse {
		var wire struct {
			Input    json.RawMessage `json:"input"`
			Previous *string         `json:"previous_response_id"`
		}
		if err := json.Unmarshal(raw, &wire); err != nil {
			return nil, err
		}
		if len(wire.Input) > 0 && wire.Input[0] == '[' {
			view.SetOpenAIRawInputItems(wire.Input)
		}
		view.PreviousResponseID = wire.Previous
	}
	return &view, nil
}

func NativeRequest(ctx context.Context, view *model.InternalLLMRequest) (*llm.Request, error) {
	if view == nil {
		return nil, errors.New("request is nil")
	}
	if view.Native != nil {
		// Isolate retries, including nested messages and provider-private fields.
		b, err := json.Marshal(view.Native)
		if err != nil {
			return nil, err
		}
		var r llm.Request
		if err := json.Unmarshal(b, &r); err != nil {
			return nil, err
		}
		r.ProviderExtensions = llm.CloneProviderExtensions(view.Native.ProviderExtensions)
		if view.Native.Image != nil {
			r.Image.ModelSpecified = view.Native.Image.ModelSpecified
			r.Image.Mask = bytes.Clone(view.Native.Image.Mask)
			r.Image.Images = make([][]byte, len(view.Native.Image.Images))
			for n, data := range view.Native.Image.Images {
				r.Image.Images[n] = bytes.Clone(data)
			}
		}
		r.Model, r.Stream = view.Model, view.Stream
		if r.RawRequest != nil {
			raw := *r.RawRequest
			raw.Query = view.Query
			r.RawRequest = &raw
		}
		if view.RawAPIFormat == model.APIFormatOpenAIResponse {
			r.PreviousResponseID = view.GetOpenAIResponsesOptions().PreviousResponseID
			if view.IsOpenAIExactReplayRequest() {
				// Session recovery changes raw input, so parse that request with AxonHub again.
				body, err := sjson.SetRawBytes(view.RawRequest, "input", view.OpenAIRawInputItems())
				if err != nil {
					return nil, err
				}
				body, _ = sjson.DeleteBytes(body, "previous_response_id")
				body, _ = sjson.DeleteBytes(body, "conversation")
				body, _ = sjson.SetBytes(body, "model", view.Model)
				return responses.NewInboundTransformer().TransformRequest(ctx, &httpclient.Request{Method: "POST", Body: body, ContentType: "application/json", Headers: http.Header{"Content-Type": {"application/json"}}, Query: view.Query})
			}
		}
		return &r, nil
	}
	// Locally generated health probes contain no provider extensions.
	if len(view.SystemOnePayload) > 0 {
		r, e := NewInbound(5).TransformRequest(ctx, view.SystemOnePayload)
		if e != nil {
			return nil, e
		}
		r.Native.Model = view.Model
		return r.Native, nil
	}
	if len(view.RerankPayload) > 0 {
		r, e := NewInbound(4).TransformRequest(ctx, view.RerankPayload)
		if e != nil {
			return nil, e
		}
		r.Native.Model = view.Model
		return r.Native, nil
	}
	b, err := json.Marshal(view)
	if err != nil {
		return nil, err
	}
	var r llm.Request
	if err := json.Unmarshal(b, &r); err != nil {
		return nil, err
	}
	if view.EmbeddingInput != nil {
		b, err = json.Marshal(map[string]any{"model": view.Model, "input": view.EmbeddingInput, "dimensions": view.EmbeddingDimensions, "encoding_format": view.EmbeddingEncodingFormat})
		if err != nil {
			return nil, err
		}
		return openai.NewEmbeddingInboundTransformer().TransformRequest(ctx, &httpclient.Request{Method: "POST", Body: b, ContentType: "application/json", Headers: make(http.Header)})
	}
	return &r, nil
}

func ProjectResponse(r *llm.Response) (*model.InternalLLMResponse, error) {
	if r == nil {
		return nil, nil
	}
	b, err := json.Marshal(r)
	if err != nil {
		return nil, err
	}
	var view model.InternalLLMResponse
	if err = json.Unmarshal(b, &view); err != nil {
		return nil, fmt.Errorf("project response: %w", err)
	}
	view.Native = r
	if r.Error != nil && view.Error != nil {
		view.Error.StatusCode = r.Error.StatusCode
	}
	if r.Embedding != nil {
		b, _ := json.Marshal(r.Embedding)
		var f struct {
			Data []model.EmbeddingObject `json:"data"`
		}
		if err = json.Unmarshal(b, &f); err != nil {
			return nil, err
		}
		view.EmbeddingData = f.Data
	}
	if r.Rerank != nil {
		view.RerankPayload, _ = json.Marshal(r.Rerank)
	}
	if r.SystemOne != nil {
		view.SystemOnePayload, _ = json.Marshal(r.SystemOne)
	}
	return &view, nil
}

func (i *Inbound) TransformResponse(ctx context.Context, view *model.InternalLLMResponse) ([]byte, error) {
	r := view.Native
	if r == nil {
		b, e := json.Marshal(view)
		if e != nil {
			return nil, e
		}
		r = &llm.Response{}
		if e = json.Unmarshal(b, r); e != nil {
			return nil, e
		}
	}
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
func (i *Inbound) StreamTerminalEvents() map[string]struct{} {
	switch i.Format {
	case model.APIFormatOpenAIResponse:
		return map[string]struct{}{"response.completed": {}, "response.failed": {}, "response.incomplete": {}}
	case model.APIFormatAnthropicMessage:
		return map[string]struct{}{"message_stop": {}}
	}
	return nil
}

type Outbound struct {
	Kind        int
	Transformer transformer.Outbound
	Request     *httpclient.Request
}

func NewOutbound(kind int) *Outbound { return &Outbound{Kind: kind} }

type staticToken string

func (s staticToken) Get(context.Context) (*oauth.OAuthCredentials, error) {
	return &oauth.OAuthCredentials{AccessToken: string(s)}, nil
}

func (o *Outbound) configure(base, key string) error {
	var err error
	switch o.Kind {
	case 0, 5:
		o.Transformer, err = openai.NewOutboundTransformer(base, key)
	case 1:
		o.Transformer, err = responses.NewOutboundTransformer(base, key)
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
	default:
		err = fmt.Errorf("unknown outbound type %d", o.Kind)
	}
	return err
}

func (o *Outbound) TransformRequest(ctx context.Context, view *model.InternalLLMRequest, base, key string) (*http.Request, error) {
	if err := o.configure(base, key); err != nil {
		return nil, err
	}
	r, err := NativeRequest(ctx, view)
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
	if !o.Request.SkipInboundQueryMerge {
		if o.Request.Query == nil {
			o.Request.Query = make(url.Values)
		}
		for k, v := range view.Query {
			if _, ok := o.Request.Query[k]; !ok {
				o.Request.Query[k] = v
			}
		}
	}
	req, err := HTTPRequest(ctx, o.Request)
	if err != nil {
		return nil, err
	}
	if o.Kind == 2 {
		providercompat.SetAnthropicAuthHeader(req.Header, base, key)
	}
	return req, nil
}

func HTTPRequest(ctx context.Context, r *httpclient.Request) (*http.Request, error) {
	u, err := url.Parse(r.URL)
	if err != nil {
		return nil, err
	}
	q := u.Query()
	for k, v := range r.Query {
		q[k] = v
	}
	u.RawQuery = q.Encode()
	req, err := http.NewRequestWithContext(ctx, r.Method, u.String(), bytes.NewReader(r.Body))
	if err != nil {
		return nil, err
	}
	req.Header = r.Headers.Clone()
	if req.Header == nil {
		req.Header = make(http.Header)
	}
	if r.ContentType != "" {
		req.Header.Set("Content-Type", r.ContentType)
	}
	if r.Auth != nil {
		switch r.Auth.Type {
		case httpclient.AuthTypeBearer:
			req.Header.Set("Authorization", "Bearer "+r.Auth.APIKey)
		case httpclient.AuthTypeAPIKey:
			req.Header.Set(r.Auth.HeaderKey, r.Auth.APIKey)
		}
	}
	return req, nil
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
	r, err := NativeRequest(context.Background(), &model.InternalLLMRequest{Model: "session", Messages: messages})
	if err != nil {
		return nil, err
	}
	array := true
	r.TransformOptions.ArrayInputs = &array
	body, err := ResponsesPayload(context.Background(), &model.InternalLLMRequest{Model: r.Model, Native: r})
	if err != nil {
		return nil, err
	}
	var wire struct {
		Input json.RawMessage `json:"input"`
	}
	err = json.Unmarshal(body, &wire)
	return wire.Input, err
}
