package stream

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/bestruirui/octopus/internal/utils/log"
	"github.com/bestruirui/octopus/internal/utils/safe"
	"github.com/tmaxmax/go-sse"
)

// ErrEmptyUpstreamStream marks 200 SSE streams that ended without forwarding
// any payload (all events skipped by transform or no events at all).
// Relay should fail over to another channel.
var ErrEmptyUpstreamStream = errors.New("upstream stream ended without forwarding any payload")

// ErrRawStreamTooLarge prevents passthrough metrics buffering from growing
// without bound. The stream limit is checked before the next chunk is written.
var ErrRawStreamTooLarge = errors.New("upstream stream exceeds raw buffer limit")

const defaultRawStreamBufferLimit = 16 * 1024 * 1024

// StreamSource abstracts different event sources (SSE, WebSocket, raw bytes).
type StreamSource interface {
	// ReadEvent blocks until the next event is available or returns an error.
	// Returns io.EOF when the stream ends normally.
	ReadEvent(ctx context.Context) ([]byte, error)

	// Close releases resources. Must be idempotent.
	Close() error
}

type streamErrorCloser interface {
	CloseWithError()
}

// StreamWriter abstracts the HTTP/WebSocket response writer.
type StreamWriter interface {
	Write(data []byte) (int, error)
	Flush()
	Written() bool
	Header() http.Header
	WriteHeader(code int)
}

// StreamConfig configures a StreamProcessor instance.
type StreamConfig struct {
	// Core dependencies
	Source  StreamSource
	Writer  StreamWriter
	Context context.Context

	// Timeout & heartbeat
	FirstTokenTimeout time.Duration // 0 to disable
	HeartbeatInterval time.Duration // 0 to disable

	// Callbacks
	OnFirstResponse func()                                            // Called when first text, reasoning, or tool content arrives
	OnFirstToken    func()                                            // Called when first meaningful payload is written
	OnFinish        func(ctx context.Context, rawStream []byte) error // Called on stream end

	// Passthrough-specific
	BufferRawStream   bool                // Enable raw stream buffering for metrics
	MaxRawBufferBytes int                 // Maximum buffered stream bytes; 0 uses a safe default
	TerminalEvents    map[string]struct{} // Protocol terminal events for early completion
}

// StreamProcessor unifies all stream handling logic.
type StreamProcessor struct {
	config StreamConfig

	// State
	rawBuffer       bytes.Buffer
	payloadWritten  bool
	firstToken      bool
	firstResponse   bool
	terminalReached bool
}

// NewStreamProcessor creates a processor from config.
func NewStreamProcessor(config StreamConfig) *StreamProcessor {
	if config.BufferRawStream && config.MaxRawBufferBytes <= 0 {
		config.MaxRawBufferBytes = defaultRawStreamBufferLimit
	}
	return &StreamProcessor{
		config:        config,
		firstToken:    true,
		firstResponse: true,
	}
}

// Run executes the unified stream processing loop.
func (p *StreamProcessor) Run() (runErr error) {
	// Set SSE response headers
	headers := p.config.Writer.Header()
	headers.Set("Content-Type", "text/event-stream")
	headers.Set("Cache-Control", "no-cache")
	headers.Set("Connection", "keep-alive")
	headers.Set("X-Accel-Buffering", "no")

	// Setup heartbeat ticker
	var heartbeatTicker *time.Ticker
	var heartbeatC <-chan time.Time
	if p.config.HeartbeatInterval > 0 {
		heartbeatTicker = time.NewTicker(p.config.HeartbeatInterval)
		heartbeatC = heartbeatTicker.C
		defer heartbeatTicker.Stop()
	}

	// Setup first token timeout
	var firstTokenTimer *time.Timer
	var firstTokenC <-chan time.Time
	if p.firstToken && p.config.FirstTokenTimeout > 0 {
		firstTokenTimer = time.NewTimer(p.config.FirstTokenTimeout)
		firstTokenC = firstTokenTimer.C
		defer func() {
			if firstTokenTimer != nil {
				firstTokenTimer.Stop()
			}
		}()
	}

	// Async read from source — use a derived context so we can unblock on any exit.
	readCtx, readCancel := context.WithCancel(p.config.Context)
	defer readCancel()
	defer func() {
		if runErr != nil {
			if errorCloser, ok := p.config.Source.(streamErrorCloser); ok {
				errorCloser.CloseWithError()
				return
			}
		}
		_ = p.config.Source.Close()
	}()

	type readResult struct {
		data []byte
		err  error
	}
	results := make(chan readResult, 1)
	safe.Go("stream-processor-read", func() {
		defer close(results)
		for {
			data, err := p.config.Source.ReadEvent(readCtx)
			select {
			case results <- readResult{data: data, err: err}:
			case <-readCtx.Done():
				return
			}
			if err != nil {
				return
			}
		}
	})

	// Main event loop
	for {
		select {
		case <-p.config.Context.Done():
			return p.handleDisconnect()

		case <-firstTokenC:
			return p.handleFirstTokenTimeout()

		case <-heartbeatC:
			if err := p.writeHeartbeat(); err != nil {
				return err
			}

		case r, ok := <-results:
			if !ok {
				// The read goroutine may close without forwarding its final
				// cancellation result when readCtx is canceled concurrently.
				// Preserve the distinction between a completed terminal stream
				// and a genuine mid-stream client disconnect.
				if p.config.Context.Err() != nil {
					return p.handleDisconnect()
				}
				// Channel closed without cancellation, stream ended normally.
				return p.finalize()
			}

			if r.err != nil {
				if r.err == io.EOF {
					return p.finalize()
				}
				// A client disconnect can unblock the source read and make its
				// cancellation result race with Context.Done(). Route that result
				// through the same terminal-event check instead of reporting a
				// completed stream as a read failure.
				if p.config.Context.Err() != nil &&
					(errors.Is(r.err, context.Canceled) || errors.Is(r.err, context.DeadlineExceeded)) {
					return p.handleDisconnect()
				}
				return fmt.Errorf("stream read error: %w", r.err)
			}

			if len(r.data) == 0 {
				continue
			}

			if p.firstResponse && isFirstResponsePayload(r.data) {
				p.firstResponse = false
				if p.config.OnFirstResponse != nil {
					p.config.OnFirstResponse()
				}
			}

			// Buffer raw data if enabled
			if p.config.BufferRawStream {
				if len(r.data) > p.config.MaxRawBufferBytes-p.rawBuffer.Len() {
					return fmt.Errorf("%w: limit=%d", ErrRawStreamTooLarge, p.config.MaxRawBufferBytes)
				}
				p.rawBuffer.Write(r.data)
			}

			// Write the event to the client.
			if err := p.processEvent(r.data); err != nil {
				return err
			}

			// First token handling
			if p.firstToken && p.payloadWritten {
				p.firstToken = false
				if p.config.OnFirstToken != nil {
					p.config.OnFirstToken()
				}
				if firstTokenTimer != nil {
					if !firstTokenTimer.Stop() {
						select {
						case <-firstTokenTimer.C:
						default:
						}
					}
					firstTokenTimer = nil
					firstTokenC = nil
				}
			}
		}
	}
}

// isFirstResponsePayload follows Magpie's TTFT semantics: the first response is
// the first event carrying content (text, reasoning, or a tool call), not an
// envelope such as response.created or an empty role/text chunk.
func isFirstResponsePayload(data []byte) bool {
	trimmed := bytes.TrimSpace(data)
	if len(trimmed) == 0 || bytes.HasPrefix(trimmed, []byte(":")) {
		return false
	}
	if bytes.Equal(trimmed, []byte("[DONE]")) || bytes.Equal(trimmed, []byte("data: [DONE]")) {
		return false
	}

	eventType, payload := firstResponseEvent(trimmed)
	if len(payload) == 0 {
		return false
	}
	var envelope map[string]any
	if json.Unmarshal(payload, &envelope) != nil {
		return false
	}
	if eventType == "" {
		if value, ok := envelope["type"].(string); ok {
			eventType = strings.TrimSpace(value)
		}
	}

	filled := func(value any) bool {
		switch typed := value.(type) {
		case nil:
			return false
		case string:
			return typed != ""
		case []any:
			return len(typed) > 0
		case map[string]any:
			return len(typed) > 0
		default:
			return true
		}
	}
	fieldFilled := func(object map[string]any, key string) bool {
		if object == nil {
			return false
		}
		return filled(object[key])
	}

	switch eventType {
	case "response.output_text.delta", "response.refusal.delta",
		"response.reasoning_summary_text.delta", "response.reasoning_text.delta",
		"response.function_call_arguments.delta", "response.custom_tool_call_input.delta":
		return fieldFilled(envelope, "delta")
	case "response.output_item.added":
		item, _ := envelope["item"].(map[string]any)
		kind, _ := item["type"].(string)
		return kind == "function_call" || kind == "custom_tool_call"
	case "content_block_start":
		block, _ := envelope["content_block"].(map[string]any)
		kind, _ := block["type"].(string)
		return kind == "tool_use" || kind == "server_tool_use"
	case "content_block_delta":
		delta, _ := envelope["delta"].(map[string]any)
		return fieldFilled(delta, "text") || fieldFilled(delta, "thinking") || fieldFilled(delta, "partial_json")
	}

	if choices, ok := envelope["choices"].([]any); ok {
		for _, raw := range choices {
			choice, _ := raw.(map[string]any)
			if choice == nil {
				continue
			}
			delta, _ := choice["delta"].(map[string]any)
			message, _ := choice["message"].(map[string]any)
			for _, content := range []map[string]any{delta, message} {
				if fieldFilled(content, "content") || fieldFilled(content, "refusal") ||
					fieldFilled(content, "reasoning") || fieldFilled(content, "reasoning_content") ||
					fieldFilled(content, "tool_calls") || fieldFilled(content, "function_call") {
					return true
				}
			}
		}
	}
	if candidates, ok := envelope["candidates"].([]any); ok {
		for _, raw := range candidates {
			candidate, _ := raw.(map[string]any)
			content, _ := candidate["content"].(map[string]any)
			parts, _ := content["parts"].([]any)
			for _, rawPart := range parts {
				part, _ := rawPart.(map[string]any)
				if text, _ := part["text"].(string); text != "" {
					return true
				}
				if fieldFilled(part, "functionCall") {
					return true
				}
			}
		}
	}
	return false
}

func firstResponseEvent(data []byte) (string, []byte) {
	var eventType string
	var payload []byte
	framed := false
	for _, line := range bytes.Split(data, []byte("\n")) {
		line = bytes.TrimSuffix(line, []byte("\r"))
		switch {
		case bytes.HasPrefix(line, []byte("event:")):
			framed = true
			eventType = strings.TrimSpace(string(line[len("event:"):]))
		case bytes.HasPrefix(line, []byte("data:")):
			framed = true
			part := bytes.TrimSpace(line[len("data:"):])
			if len(part) > 0 {
				payload = append(payload, part...)
			}
		}
	}
	if !framed {
		payload = bytes.TrimSpace(data)
	}
	return eventType, payload
}

// processEvent writes a single event. Bytes arriving here are already in the
// client's wire format: AxonHub's inbound transformer produced them.
func (p *StreamProcessor) processEvent(data []byte) error {
	output := data

	if _, err := p.config.Writer.Write(output); err != nil {
		return fmt.Errorf("write error: %w", err)
	}

	p.payloadWritten = p.config.Writer.Written()
	if !p.terminalReached && len(p.config.TerminalEvents) > 0 {
		p.terminalReached = streamContainsTerminal(output, p.config.TerminalEvents)
	}
	p.config.Writer.Flush()
	return nil
}

// writeHeartbeat sends SSE heartbeat (comment line).
func (p *StreamProcessor) writeHeartbeat() error {
	if _, err := p.config.Writer.Write([]byte(":\n\n")); err != nil {
		return err
	}
	p.config.Writer.Flush()
	return nil
}

// handleDisconnect handles context cancellation or timeout.
func (p *StreamProcessor) handleDisconnect() error {
	// Transformed streams are checked after a successful client write. Raw
	// passthrough streams retain the buffered fallback for split SSE events.
	if p.terminalReached || (p.config.BufferRawStream && p.streamReachedTerminal()) {
		log.Debugf("client disconnected but stream reached terminal event, treating as success")
		return p.finalize()
	}

	err := p.config.Context.Err()
	log.Debugf("client disconnected, stopping stream: written=%t first_token_seen=%t err=%v",
		p.payloadWritten, !p.firstToken, err)

	if p.config.BufferRawStream && p.rawBuffer.Len() > 0 {
		// Still call OnFinish to collect partial metrics
		if p.config.OnFinish != nil {
			_ = p.config.OnFinish(context.Background(), p.rawBuffer.Bytes())
		}
	}

	return err
}

// handleFirstTokenTimeout returns first token timeout error.
func (p *StreamProcessor) handleFirstTokenTimeout() error {
	log.Warnf("first token timeout (%v), switching channel", p.config.FirstTokenTimeout)
	return fmt.Errorf("first token timeout after %v", p.config.FirstTokenTimeout)
}

// finalize completes the stream and calls OnFinish callback.
func (p *StreamProcessor) finalize() error {
	if !p.payloadWritten {
		return ErrEmptyUpstreamStream
	}

	log.Debugf("stream end (payload_written=%t)", p.payloadWritten)

	if p.config.OnFinish != nil {
		rawStream := p.rawBuffer.Bytes()
		if err := p.config.OnFinish(p.config.Context, rawStream); err != nil {
			return err
		}
	}

	return nil
}

// PayloadWritten returns whether any payload has been written to the client.
func (p *StreamProcessor) PayloadWritten() bool {
	return p.payloadWritten
}

// streamReachedTerminal checks if buffered stream contains a terminal event.
func (p *StreamProcessor) streamReachedTerminal() bool {
	if p.rawBuffer.Len() == 0 {
		return false
	}
	return streamContainsTerminal(p.rawBuffer.Bytes(), p.config.TerminalEvents)
}

func streamContainsTerminal(data []byte, terminalEvents map[string]struct{}) bool {
	if len(data) == 0 || len(terminalEvents) == 0 {
		return false
	}

	readCfg := &sse.ReadConfig{MaxEventSize: 32 * 1024 * 1024}
	for ev, err := range sse.Read(bytes.NewReader(data), readCfg) {
		if err != nil {
			break
		}

		// Extract event type
		typ := strings.TrimSpace(ev.Type)
		if typ == "" {
			data := ev.Data
			if len(data) > 0 && data[0] == '{' {
				typ = extractJSONType(data)
			}
		}

		if _, ok := terminalEvents[typ]; ok {
			return true
		}
	}

	return false
}

// extractJSONType extracts "type" field from JSON without full unmarshaling.
func extractJSONType(data string) string {
	// Simple extraction: find "type":"value"
	if idx := strings.Index(data, `"type"`); idx >= 0 {
		rest := data[idx+6:]
		if idx := strings.IndexByte(rest, ':'); idx >= 0 {
			rest = rest[idx+1:]
			rest = strings.TrimSpace(rest)
			if len(rest) > 0 && rest[0] == '"' {
				rest = rest[1:]
				if idx := strings.IndexByte(rest, '"'); idx >= 0 {
					return rest[:idx]
				}
			}
		}
	}
	return ""
}
