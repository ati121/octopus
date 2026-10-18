package relay

import (
	"context"
	"fmt"

	"github.com/bestruirui/octopus/internal/relay/stream"
	"github.com/bestruirui/octopus/internal/transformer/axon"
	"github.com/looplj/axonhub/llm/httpclient"
	"github.com/looplj/axonhub/llm/streams"
)

func (ra *relayAttempt) transformSource(ctx context.Context, source stream.StreamSource) (*axon.Pipeline, error) {
	out, ok := ra.outAdapter.(*axon.Outbound)
	if !ok {
		_ = source.Close()
		return nil, fmt.Errorf("relay requires AxonHub transformers")
	}
	return ra.transformSourceWithAdapter(ctx, source, out)
}

// transformEvents builds the pipeline over an already-decoded AxonHub event
// stream, which is what the WebSocket executor produces.
func (ra *relayAttempt) transformEvents(ctx context.Context, events streams.Stream[*httpclient.StreamEvent], out *axon.Outbound, transportErr func() error) (*axon.Pipeline, error) {
	in, inOK := ra.inAdapter.(*axon.Inbound)
	if !inOK || out == nil {
		return nil, fmt.Errorf("relay requires AxonHub transformers")
	}
	return axon.NewPipelineFromEvents(ctx, in, out, events, transportErr, maxRawStreamBufferSize)
}

// transformSourceWithAdapter builds the pipeline with an explicit outbound
// adapter. The WebSocket path needs this because the event stream was produced
// by an outbound configured for a different transport.
func (ra *relayAttempt) transformSourceWithAdapter(ctx context.Context, source stream.StreamSource, out *axon.Outbound) (*axon.Pipeline, error) {
	in, inOK := ra.inAdapter.(*axon.Inbound)
	if !inOK || out == nil {
		_ = source.Close()
		return nil, fmt.Errorf("relay requires AxonHub transformers")
	}
	pipeline, err := axon.NewPipeline(ctx, in, out, source, maxRawStreamBufferSize)
	if err != nil {
		_ = source.Close()
	}
	return pipeline, err
}
