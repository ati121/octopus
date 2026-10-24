package axon

import (
	"bytes"
	"context"
	"io"
	"net/http"

	"github.com/bestruirui/octopus/internal/transformer/model"
	"github.com/looplj/axonhub/llm/httpclient"
	"github.com/looplj/axonhub/llm/streams"
)

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
		r.Usage = meta.Usage
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
