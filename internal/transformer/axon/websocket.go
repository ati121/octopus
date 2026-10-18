package axon

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"

	"github.com/looplj/axonhub/llm/httpclient"
	"github.com/looplj/axonhub/llm/streams"
	"github.com/looplj/axonhub/llm/transformer/openai/responses"
	"github.com/looplj/axonhub/llm/transformer/shared"
)

// WebSocketTransport forwards Responses API requests over an upstream
// WebSocket using AxonHub's executor. Connection pooling, response.create
// framing, previous_response_id continuation and terminal-event handling all
// belong to the library; Octopus only owns routing, timing and observation.
type WebSocketTransport struct {
	executor *responses.WebSocketExecutor
}

var (
	transportMu     sync.Mutex
	transportByChan = map[string]*WebSocketTransport{}
)

// WebSocketTransportFor returns a process-wide transport for one channel key.
// AxonHub keeps the WebSocket pool inside the executor, so the executor must
// outlive individual requests.
func WebSocketTransportFor(scope string, proxy *httpclient.ProxyConfig) (*WebSocketTransport, error) {
	if scope == "" {
		return nil, errors.New("websocket scope is required")
	}
	transportMu.Lock()
	defer transportMu.Unlock()
	if existing, ok := transportByChan[scope]; ok {
		return existing, nil
	}
	inner := httpclient.NewHttpClientWithProxy(proxy)
	transport := &WebSocketTransport{executor: responses.NewWebSocketExecutor(inner)}
	transportByChan[scope] = transport
	return transport, nil
}

// Close releases every pooled upstream WebSocket.
func (t *WebSocketTransport) Close() error {
	if t == nil || t.executor == nil {
		return nil
	}
	return t.executor.Close()
}

// DoStream issues one response.create and returns AxonHub's event stream.
func (t *WebSocketTransport) DoStream(ctx context.Context, request *httpclient.Request) (streams.Stream[*httpclient.StreamEvent], error) {
	if t == nil || t.executor == nil {
		return nil, errors.New("websocket transport is not initialized")
	}
	return t.executor.DoStream(ctx, request)
}

// ResetWebSocketTransportsForTest drops every cached transport. Tests point
// channels at ephemeral servers, so a transport must never survive a test.
func ResetWebSocketTransportsForTest() {
	CloseAllWebSocketTransports()
}

// CloseAllWebSocketTransports drains every pooled upstream session. It runs at
// shutdown so idle WebSockets do not outlive the process gracefully.
func CloseAllWebSocketTransports() {
	transportMu.Lock()
	transports := make([]*WebSocketTransport, 0, len(transportByChan))
	for _, transport := range transportByChan {
		transports = append(transports, transport)
	}
	transportByChan = map[string]*WebSocketTransport{}
	transportMu.Unlock()
	for _, transport := range transports {
		_ = transport.Close()
	}
}

// WebSocketScope derives a stable pool scope. It must be unique per channel key
// and per upstream endpoint: if a channel's base URL changes, connections to the
// old host must not be reused.
func WebSocketScope(channelID, keyID int, baseURL string) string {
	return fmt.Sprintf("octopus:%d:%d:%s", channelID, keyID, strings.TrimRight(strings.TrimSpace(baseURL), "/"))
}

// WebSocketSessionContext attaches the session identity AxonHub needs before it
// will pool a WebSocket connection. Octopus uses the downstream session or the
// previous response ID as the pool identity.
func WebSocketSessionContext(ctx context.Context, sessionID, scope string) context.Context {
	ctx = shared.WithSessionID(ctx, sessionID)
	ctx = shared.WithSessionScope(ctx, scope)
	return shared.WithResponsesAPI(ctx)
}
