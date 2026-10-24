package relay

import (
	"bytes"
	"context"
	"errors"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/bestruirui/octopus/internal/helper"
	"github.com/bestruirui/octopus/internal/transformer/axon"
	"github.com/bestruirui/octopus/internal/utils/iolimit"
	"github.com/looplj/axonhub/llm"
	"github.com/looplj/axonhub/llm/httpclient"
	axonpipeline "github.com/looplj/axonhub/llm/pipeline"
	"github.com/looplj/axonhub/llm/streams"
)

func (ra *relayAttempt) runPipeline(ctx context.Context, out *axon.Outbound, executor axonpipeline.Executor) (int, error) {
	// HTTP fallback after a pre-content WS failure starts a new collection.
	ra.responseCollected.Store(false)
	ra.metrics.ResetAttemptResponse()
	ra.deferredStreamPayload = nil
	native, err := axon.NativeRequest(ctx, ra.internalRequest)
	if err != nil {
		return 0, err
	}
	in := ra.inAdapter.(*axon.Inbound)
	execution, err := axon.Execute(ctx, in.Transformer, out, native, executor, maxRawStreamBufferSize, func(ctx context.Context, wire *httpclient.Request) (*httpclient.Request, error) {
		if ra.channel != nil {
			ra.copyHeaders(&http.Request{Header: wire.Headers})
			if err := ra.applyParamOverrideToWire(wire); err != nil {
				return nil, err
			}
		}
		if finalizer, ok := out.Transformer.(interface {
			FinalizeTransportRequest(*httpclient.Request) *httpclient.Request
		}); ok {
			wire = finalizer.FinalizeTransportRequest(wire)
		}
		return wire, nil
	})
	in.SetCollector(execution.Collect)
	if err != nil {
		ra.collectResponse()
		if timeoutErr := ra.firstTokenTimeoutIfNeeded(ctx, err); timeoutErr != nil {
			return 0, timeoutErr
		}
		var responseErr *llm.ResponseError
		if errors.As(err, &responseErr) {
			if ra.internalRequest.Stream != nil && *ra.internalRequest.Stream && ctx.Err() == nil {
				failure, renderErr := in.Transformer.TransformStream(ctx, streams.MapErr(streams.SliceStream([]*llm.Response{{}}), func(*llm.Response) (*llm.Response, error) { return nil, responseErr }))
				if renderErr == nil {
					var body bytes.Buffer
					for failure.Next() {
						_ = axon.WriteEvent(&body, failure.Current())
					}
					if failure.Err() == nil {
						ra.deferredStreamPayload = body.Bytes()
					}
					_ = failure.Close()
				}
			}
			return normalizeUpstreamStatusCode(responseErr.StatusCode, responseErr.Error()), err
		}
		return 0, err
	}
	if execution.Result.Stream {
		copyProxyResponseHeaders(ra.getStreamWriter().Header(), httpclient.GetResponseHeaders(execution.Result.EventStream))
		err = ra.writePipelineStream(ctx, execution.Result.EventStream)
		ra.collectResponse()
		if isClientCancellation(ctx, err) && streamResponseCompleted(ra.metrics.InternalResponse) {
			err = nil
		}
		return http.StatusOK, err
	}
	ra.collectResponse()
	result := execution.Result.Response
	copyProxyResponseHeaders(ra.c.Writer.Header(), result.Headers)
	ra.c.Data(result.StatusCode, result.Headers.Get("Content-Type"), result.Body)
	return result.StatusCode, nil
}

func (ra *relayAttempt) writePipelineStream(ctx context.Context, events streams.Stream[*httpclient.StreamEvent]) error {
	ra.heartbeat.Hand()
	writer := ra.getStreamWriter()
	writer.Header().Set("Content-Type", "text/event-stream")
	writer.Header().Set("Cache-Control", "no-cache")
	writer.Header().Set("X-Accel-Buffering", "no")
	type result struct {
		event *httpclient.StreamEvent
		err   error
		done  bool
	}
	reads := make(chan result, 1)
	readCtx, cancel := context.WithCancel(ctx)
	defer cancel()
	done := make(chan struct{})
	defer func() { cancel(); _ = events.Close(); <-done }()
	go func() {
		defer close(done)
		defer close(reads)
		for events.Next() {
			select {
			case reads <- result{event: events.Current()}:
			case <-readCtx.Done():
				return
			}
		}
		select {
		case reads <- result{err: events.Err(), done: true}:
		case <-readCtx.Done():
		}
	}()
	ticker, heartbeat := newStreamHeartbeatTicker()
	if ticker != nil {
		defer ticker.Stop()
	}
	for {
		select {
		case <-ctx.Done():
			return contextError(ctx)
		case <-heartbeat:
			if err := writeSSEHeartbeat(writer); err != nil {
				return err
			}
		case r, ok := <-reads:
			if !ok {
				return nil
			}
			if r.done {
				return r.err
			}
			if r.event == nil {
				continue
			}
			write := func() (bool, error) {
				var err error
				if direct, ok := writer.(interface {
					WriteEvent(*httpclient.StreamEvent) error
				}); ok {
					err = direct.WriteEvent(r.event)
				} else {
					err = axon.WriteEvent(writer, r.event)
				}
				if err != nil {
					return false, err
				}
				writer.Flush()
				if !ra.streamPayloadWritten.Swap(true) {
					ra.metrics.SetFirstTokenTime(time.Now())
					ra.stopFirstTokenTimer()
				}
				return true, nil
			}
			var err error
			if boundary, ok := events.(streams.DeliveryBoundary); ok {
				err = boundary.Deliver(ctx, write)
			} else {
				_, err = write()
			}
			if err != nil {
				return err
			}
		}
	}
}

func (ra *relayAttempt) pipelineHTTP(ctx context.Context) (int, error) {
	out := ra.outAdapter.(*axon.Outbound)
	if err := out.Configure(ra.channel.GetBaseUrl(), ra.usedKey.ChannelKey); err != nil {
		return 0, err
	}
	client, err := helper.ChannelHTTPClientWithContext(ctx, ra.channel)
	if err != nil {
		return 0, err
	}
	managedClient := *client
	transport := client.Transport
	if transport == nil {
		transport = http.DefaultTransport
	}
	managedClient.Transport = observedTransport{base: transport, ra: ra}
	budget := ra.attachFirstTokenBudget((&http.Request{}).WithContext(ctx))
	budget = ra.attachRequestTimeout(budget)
	defer ra.closeFirstTokenBudget()
	executor := httpclient.NewHttpClientWithClient(&managedClient)
	return ra.runPipeline(budget.Context(), out, executor)
}

// Preserve retry-after as management metadata without parsing provider bodies.
type observedTransport struct {
	base http.RoundTripper
	ra   *relayAttempt
}

func (t observedTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	if req.Header.Get("User-Agent") == "axonhub/1.0" {
		req.Header.Set("User-Agent", "")
	}
	response, err := t.base.RoundTrip(req)
	if response != nil {
		t.ra.retryAfter = parseRetryAfter(response.Header.Get("Retry-After"))
		if response.StatusCode < 400 && !strings.Contains(strings.ToLower(response.Header.Get("Content-Type")), "text/event-stream") {
			response.Body = &boundedResponseBody{ReadCloser: response.Body, remaining: iolimit.UpstreamResponseMaxBytes(), limit: iolimit.UpstreamResponseMaxBytes()}
		}
	}
	return response, err
}

type boundedResponseBody struct {
	io.ReadCloser
	remaining, limit int64
}

func (b *boundedResponseBody) Read(p []byte) (int, error) {
	if int64(len(p)) > b.remaining+1 {
		p = p[:b.remaining+1]
	}
	n, err := b.ReadCloser.Read(p)
	b.remaining -= int64(n)
	if b.remaining < 0 {
		return 0, &iolimit.TooLargeError{Limit: b.limit}
	}
	return n, err
}
