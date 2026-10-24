package axon

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"

	"github.com/bestruirui/octopus/internal/transformer/model"
	"github.com/looplj/axonhub/llm"
	"github.com/looplj/axonhub/llm/httpclient"
	"github.com/looplj/axonhub/llm/pipeline"
	streamusage "github.com/looplj/axonhub/llm/pipeline/stream"
	"github.com/looplj/axonhub/llm/streams"
	"github.com/looplj/axonhub/llm/transformer"
)

type preparedInbound struct {
	transformer.Inbound
	request *llm.Request
}

func (i preparedInbound) TransformRequest(context.Context, *httpclient.Request) (*llm.Request, error) {
	return i.request, nil
}

// A retry budget keeps protocol metadata private until meaningful output. The
// management layer decides which channel/key owns the next attempt.
type managedOutbound struct{ transformer.Outbound }

func (managedOutbound) CanRetry(error) bool                   { return false }
func (managedOutbound) PrepareForRetry(context.Context) error { return nil }
func (o managedOutbound) CustomizeExecutor(executor pipeline.Executor) pipeline.Executor {
	if _, ok := executor.(*WebSocketTransport); ok {
		return executor
	}
	if custom, ok := o.Outbound.(pipeline.ChannelCustomizedExecutor); ok {
		return custom.CustomizeExecutor(executor)
	}
	return executor
}

type Execution struct {
	Result    *pipeline.Result
	Response  *model.InternalLLMResponse
	chunks    []*httpclient.StreamEvent
	outbound  *Outbound
	size      int
	limit     int
	rawOutput json.RawMessage
}

func (e *Execution) Collect(ctx context.Context) (*model.InternalLLMResponse, error) {
	if e.Response != nil {
		return e.Response, nil
	}
	response, err := e.outbound.aggregate(ctx, e.chunks)
	if err == nil {
		e.Response = response
	}
	return response, err
}

type recordingStream struct {
	streams.Stream[*httpclient.StreamEvent]
	execution *Execution
	err       error
}

func (s *recordingStream) Next() bool {
	if s.err != nil || !s.Stream.Next() {
		return false
	}
	event := s.Stream.Current()
	if event != nil {
		s.execution.size += len(event.Data)
		if s.execution.limit > 0 && s.execution.size > s.execution.limit {
			s.err = fmt.Errorf("upstream stream exceeds aggregation buffer limit")
			return false
		}
		s.execution.chunks = append(s.execution.chunks, event)
	}
	return true
}
func (s *recordingStream) Err() error {
	if s.err != nil {
		return s.err
	}
	return s.Stream.Err()
}
func (s *recordingStream) AcknowledgeDelivery() {
	if v, ok := s.Stream.(streams.DeliveryAcknowledger); ok {
		v.AcknowledgeDelivery()
	}
}
func (s *recordingStream) DeliveryAcknowledged() bool {
	v, ok := s.Stream.(streams.DeliveryAcknowledger)
	return ok && v.DeliveryAcknowledged()
}
func (s *recordingStream) Deliver(ctx context.Context, write func() (bool, error)) error {
	if v, ok := s.Stream.(streams.DeliveryBoundary); ok {
		return v.Deliver(ctx, write)
	}
	_, err := write()
	return err
}
func (s *recordingStream) WaitForFinalization() {
	if v, ok := s.Stream.(streams.FinalizationWaiter); ok {
		v.WaitForFinalization()
	}
}

func Execute(ctx context.Context, in transformer.Inbound, out *Outbound, request *llm.Request, executor pipeline.Executor, limit int, prepare func(context.Context, *httpclient.Request) (*httpclient.Request, error)) (*Execution, error) {
	e := &Execution{outbound: out, limit: limit}
	capture := pipeline.OnRawStream("relay-log", func(_ context.Context, s streams.Stream[*httpclient.StreamEvent]) (streams.Stream[*httpclient.StreamEvent], error) {
		return &recordingStream{Stream: s, execution: e}, nil
	})
	response := &responseObserver{execution: e}
	middleware := []pipeline.Middleware{streamusage.EnsureUsage(), capture, response}
	middleware = append(middleware, pipeline.OnRawRequest("relay-settings", func(ctx context.Context, wire *httpclient.Request) (*httpclient.Request, error) {
		var err error
		if prepare != nil {
			wire, err = prepare(ctx, wire)
		}
		if err == nil {
			out.Request = wire
		}
		return wire, err
	}))
	p := pipeline.NewFactory(executor).Pipeline(preparedInbound{Inbound: in, request: request}, managedOutbound{out.Transformer}, pipeline.WithEmptyResponseDetection(), pipeline.WithRetry(0, 1, 0), pipeline.WithMiddlewares(middleware...))
	raw := request.RawRequest
	if raw == nil {
		raw = &httpclient.Request{Headers: make(http.Header)}
	}
	result, err := p.Process(ctx, raw)
	if err != nil {
		return e, err
	}
	e.Result = result
	return e, nil
}

type responseObserver struct {
	pipeline.DummyMiddleware
	execution *Execution
}

func (*responseObserver) Name() string { return "relay-response" }
func (m *responseObserver) OnOutboundRawResponse(_ context.Context, r *httpclient.Response) (*httpclient.Response, error) {
	if m.execution.outbound.Kind == 1 || m.execution.outbound.Kind == 6 {
		var v struct {
			Output json.RawMessage `json:"output"`
		}
		if json.Unmarshal(r.Body, &v) == nil {
			m.execution.rawOutput = v.Output
		}
	}
	return r, nil
}
func (m *responseObserver) OnOutboundLlmResponse(_ context.Context, r *llm.Response) (*llm.Response, error) {
	m.execution.Response = &model.InternalLLMResponse{Response: *r, RawResponsesOutputItems: m.execution.rawOutput}
	return r, nil
}

// WriteEvent performs framing only; all protocol interpretation stays upstream.
func WriteEvent(w io.Writer, event *httpclient.StreamEvent) error {
	if event == nil {
		return nil
	}
	if event.IsBinaryAudioChunk() {
		_, err := w.Write(event.Data)
		return err
	}
	var b bytes.Buffer
	if event.Type != "" {
		fmt.Fprintf(&b, "event: %s\n", event.Type)
	}
	if event.LastEventID != "" {
		fmt.Fprintf(&b, "id: %s\n", event.LastEventID)
	}
	for _, line := range bytes.Split(event.Data, []byte("\n")) {
		b.WriteString("data: ")
		b.Write(line)
		b.WriteByte('\n')
	}
	b.WriteByte('\n')
	_, err := w.Write(b.Bytes())
	return err
}
