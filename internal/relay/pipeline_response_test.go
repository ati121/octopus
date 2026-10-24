package relay

import (
	"context"
	"io"
	"net/http"

	"github.com/bestruirui/octopus/internal/transformer/axon"
	"github.com/looplj/axonhub/llm/httpclient"
	"github.com/looplj/axonhub/llm/streams"
)

// The same executor path handles pre-opened HTTP bodies in transport regressions.
func (ra *relayAttempt) pipelineResponse(ctx context.Context, response *http.Response) error {
	out := ra.outAdapter.(*axon.Outbound)
	if out.Transformer == nil {
		if err := out.Configure("https://unused.invalid/v1", "unused"); err != nil {
			return err
		}
	}
	_, err := ra.runPipeline(ctx, out, responseExecutor{Response: response})
	return err
}

// Response regressions use the same pipeline with an already-open body.
func (ra *relayAttempt) handleStreamResponseV2(ctx context.Context, response *http.Response) error {
	return ra.pipelineResponse(ctx, response)
}
func (ra *relayAttempt) handleResponse(ctx context.Context, response *http.Response) error {
	return ra.pipelineResponse(ctx, response)
}

// responseExecutor supports an already-open response in transport regression tests.
type responseExecutor struct{ Response *http.Response }

func (e responseExecutor) Do(_ context.Context, r *httpclient.Request) (*httpclient.Response, error) {
	defer e.Response.Body.Close()
	b, err := io.ReadAll(e.Response.Body)
	return &httpclient.Response{StatusCode: e.Response.StatusCode, Headers: e.Response.Header, Body: b, Request: r}, err
}
func (e responseExecutor) DoStream(ctx context.Context, _ *httpclient.Request) (streams.Stream[*httpclient.StreamEvent], error) {
	return httpclient.NewDefaultSSEDecoder(ctx, e.Response.Body), nil
}
