package relay

import (
	"context"
	"fmt"
	"github.com/bestruirui/octopus/internal/relay/stream"
	"github.com/bestruirui/octopus/internal/transformer/axon"
)

func (ra *relayAttempt) transformSource(ctx context.Context, source stream.StreamSource) (*axon.Pipeline, error) {
	in, inOK := ra.inAdapter.(*axon.Inbound)
	out, outOK := ra.outAdapter.(*axon.Outbound)
	if !inOK || !outOK {
		_ = source.Close()
		return nil, fmt.Errorf("relay requires AxonHub transformers")
	}
	pipeline, err := axon.NewPipeline(ctx, in, out, source, maxRawStreamBufferSize)
	if err != nil {
		_ = source.Close()
	}
	return pipeline, err
}
