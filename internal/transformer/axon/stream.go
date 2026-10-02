package axon

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"sync"

	"github.com/bestruirui/octopus/internal/transformer/model"
	"github.com/looplj/axonhub/llm"
	"github.com/looplj/axonhub/llm/httpclient"
	"github.com/looplj/axonhub/llm/streams"
	"github.com/tidwall/gjson"
)

type Source interface {
	ReadEvent(context.Context) ([]byte, error)
	Close() error
}

type eventStream struct {
	ctx          context.Context
	source       Source
	current      *httpclient.StreamEvent
	err          error
	mu           sync.Mutex
	chunks       []*httpclient.StreamEvent
	size         int
	limit        int
	transportErr error
}

func (s *eventStream) Next() bool {
	b, err := s.source.ReadEvent(s.ctx)
	if err != nil {
		s.mu.Lock()
		s.transportErr = err
		s.mu.Unlock()
		if err != io.EOF {
			s.err = err
		}
		return false
	}
	s.current = &httpclient.StreamEvent{Data: append([]byte(nil), b...), Type: gjson.GetBytes(b, "type").String()}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.size += len(b)
	if s.limit > 0 && s.size > s.limit {
		s.err = fmt.Errorf("upstream stream exceeds aggregation buffer limit")
		return false
	}
	s.chunks = append(s.chunks, s.current)
	return true
}
func (s *eventStream) Current() *httpclient.StreamEvent { return s.current }
func (s *eventStream) Err() error                       { return s.err }
func (s *eventStream) Close() error                     { return s.source.Close() }

type Pipeline struct {
	output streams.Stream[*httpclient.StreamEvent]
	source Source
	raw    *eventStream
}

// NewPipeline creates one stateful AxonHub iterator for the entire response.
// Octopus retains its timeout, heartbeat, cancellation and deferred-commit writer.
func NewPipeline(ctx context.Context, in *Inbound, out *Outbound, source Source, limit int) (*Pipeline, error) {
	if out.Transformer == nil {
		if err := out.configure("https://unused.invalid/v1", "unused"); err != nil {
			return nil, err
		}
	}
	if out.Request == nil {
		out.Request = &httpclient.Request{APIFormat: string(out.Transformer.APIFormat()), RequestType: "chat"}
	}
	raw := &eventStream{ctx: ctx, source: source, limit: limit}
	native, err := out.Transformer.TransformStream(ctx, out.Request, raw)
	if err != nil {
		return nil, err
	}
	output, err := in.Transformer.TransformStream(ctx, native)
	if err != nil {
		_ = native.Close()
		return nil, err
	}
	in.mu.Lock()
	in.collect = func(ctx context.Context) (*model.InternalLLMResponse, error) {
		raw.mu.Lock()
		chunks := append([]*httpclient.StreamEvent(nil), raw.chunks...)
		raw.mu.Unlock()
		return out.aggregate(ctx, chunks)
	}
	in.mu.Unlock()
	return &Pipeline{output: output, source: source, raw: raw}, nil
}
func (p *Pipeline) TransportError() error {
	p.raw.mu.Lock()
	defer p.raw.mu.Unlock()
	if errors.Is(p.raw.transportErr, io.EOF) {
		return llm.ErrStreamIncomplete
	}
	return p.raw.transportErr
}
func (p *Pipeline) ReadEvent(context.Context) ([]byte, error) {
	if !p.output.Next() {
		if err := p.output.Err(); err != nil {
			return nil, err
		}
		return nil, io.EOF
	}
	e := p.output.Current()
	if e == nil {
		return nil, nil
	}
	var b bytes.Buffer
	if e.Type != "" {
		fmt.Fprintf(&b, "event: %s\n", e.Type)
	}
	for _, line := range bytes.Split(e.Data, []byte("\n")) {
		b.WriteString("data: ")
		b.Write(line)
		b.WriteByte('\n')
	}
	b.WriteByte('\n')
	return b.Bytes(), nil
}
func (p *Pipeline) Close() error { return p.output.Close() }
func (p *Pipeline) CloseWithError() {
	if s, ok := p.source.(interface{ CloseWithError() }); ok {
		s.CloseWithError()
	}
	_ = p.Close()
}

func (o *Outbound) aggregate(ctx context.Context, chunks []*httpclient.StreamEvent) (*model.InternalLLMResponse, error) {
	if len(chunks) == 0 {
		return nil, nil
	}
	b, meta, err := o.Transformer.AggregateStreamChunks(ctx, o.Request, chunks)
	if err != nil {
		return nil, err
	}
	r, err := o.TransformResponse(ctx, &http.Response{StatusCode: 200, Header: make(http.Header), Body: io.NopCloser(bytes.NewReader(b))})
	if err != nil {
		return nil, err
	}
	if meta.Usage != nil {
		rawOutput := r.RawResponsesOutputItems
		r.Native.Usage = meta.Usage
		r, err = ProjectResponse(r.Native)
		if err == nil {
			r.RawResponsesOutputItems = rawOutput
		}
	}
	return r, err
}

// Providers may stream even for a non-streaming client. Decode, validate and
// aggregate through AxonHub before the inbound transformer renders client JSON.
func (o *Outbound) aggregateSSE(ctx context.Context, body []byte) (*model.InternalLLMResponse, error) {
	decoder := httpclient.NewDefaultSSEDecoder(ctx, io.NopCloser(bytes.NewReader(body)))
	defer decoder.Close()
	chunks, err := streams.All(decoder)
	if err != nil {
		return nil, err
	}
	native, err := o.Transformer.TransformStream(ctx, o.Request, streams.SliceStream(chunks))
	if err != nil {
		return nil, err
	}
	defer native.Close()
	for native.Next() {
		_ = native.Current()
	}
	if err := native.Err(); err != nil {
		return nil, err
	}
	return o.aggregate(ctx, chunks)
}
