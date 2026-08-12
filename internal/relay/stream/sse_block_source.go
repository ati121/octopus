package stream

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"time"
)

// ErrSSEBlockTooLarge marks a single SSE event block exceeding the size cap.
var ErrSSEBlockTooLarge = errors.New("SSE event block exceeds size limit")

// sseBlockFlushGrace 是「pending 里已有未完成块、但读不出新数据」时的宽限时间。
// 超时后先把已有字节 flush 出去，避免上游在最后一个事件后迟迟不关闭连接（不
// 发 EOF）时 pending 滞留造成死锁。字节保真：客户端按连续字节流重组，切块边界
// 对客户端不可见，flush 不改变任何字节。
const sseBlockFlushGrace = 500 * time.Millisecond

// SSEBlockSource reads raw bytes and splits them into complete SSE event
// blocks, preserving each block's original framing (data: lines, event:
// lines, blank-line separators) byte-for-byte.
//
// Passthrough event interception needs whole blocks: RawSource chunks can
// split an event across reads, while SSESource discards event: lines and
// comment frames. Blocks that the interceptor leaves untouched must reach the
// client exactly as the upstream sent them.
type SSEBlockSource struct {
	reader  io.ReadCloser
	maxSize int
	pending []byte
	flushed bool

	// reading carries an in-flight bounded read started when pending held an
	// incomplete block (see sseBlockFlushGrace). Results must be drained
	// before any new read, otherwise bytes would be lost.
	reading *inFlightRead
	tmp     []byte
}

type inFlightRead struct {
	ch chan readResult
}

type readResult struct {
	n   int
	err error
}

// NewSSEBlockSource creates a source that yields one complete SSE block per
// ReadEvent. Blocks are split at "\n\n" (or "\r\n\r\n"); an incomplete
// trailing block is flushed when the stream ends or stalls for
// sseBlockFlushGrace.
func NewSSEBlockSource(reader io.ReadCloser, maxBlockSize int) *SSEBlockSource {
	if maxBlockSize <= 0 {
		maxBlockSize = 32 * 1024 * 1024 // 32MB default
	}
	return &SSEBlockSource{
		reader:  reader,
		maxSize: maxBlockSize,
		tmp:     make([]byte, 32*1024),
	}
}

// ReadEvent reads the next complete SSE block including its framing.
func (s *SSEBlockSource) ReadEvent(ctx context.Context) ([]byte, error) {
	if s.flushed {
		return nil, io.EOF
	}
	for {
		// 上一轮宽限读的结果最终会到达；阻塞等它，保证字节不丢。
		if s.reading != nil {
			r, _ := <-s.reading.ch
			s.reading = nil
			if r.n > 0 {
				s.pending = append(s.pending, s.tmp[:r.n]...)
				continue
			}
			if r.err == nil {
				continue
			}
			if errors.Is(r.err, io.EOF) {
				s.flushed = true
				return s.flushTail()
			}
			return nil, r.err
		}

		if idx := sseBlockEndIndex(s.pending); idx >= 0 {
			if idx > s.maxSize {
				return nil, fmt.Errorf("%w: limit=%d", ErrSSEBlockTooLarge, s.maxSize)
			}
			block := make([]byte, idx)
			copy(block, s.pending[:idx])
			s.pending = s.pending[idx:]
			return block, nil
		}
		if len(s.pending) > s.maxSize {
			return nil, fmt.Errorf("%w: limit=%d", ErrSSEBlockTooLarge, s.maxSize)
		}

		if len(s.pending) == 0 {
			// 常规路径：同步阻塞读，与 RawSource 行为一致。
			n, err := s.reader.Read(s.tmp)
			if n > 0 {
				s.pending = append(s.pending, s.tmp[:n]...)
				continue
			}
			if err == nil {
				continue // spurious zero-byte read without error
			}
			if errors.Is(err, io.EOF) {
				s.flushed = true
				return s.flushTail()
			}
			return nil, err
		}

		// pending 中有未完成块：带宽限读。读不到就先把 pending flush 出去并
		// 保留在途读的结果，下一轮 ReadEvent 等它到达再处理。
		readCh := make(chan readResult, 1)
		go func() {
			n, err := s.reader.Read(s.tmp)
			readCh <- readResult{n: n, err: err}
		}()
		select {
		case r := <-readCh:
			if r.n > 0 {
				s.pending = append(s.pending, s.tmp[:r.n]...)
				continue
			}
			if r.err == nil {
				continue
			}
			if errors.Is(r.err, io.EOF) {
				s.flushed = true
				return s.flushTail()
			}
			return nil, r.err
		case <-time.After(sseBlockFlushGrace):
			s.reading = &inFlightRead{ch: readCh}
			block := make([]byte, len(s.pending))
			copy(block, s.pending)
			s.pending = nil
			return block, nil
		}
	}
}

// flushTail returns the remaining pending block, or io.EOF when nothing is
// left. Called only after the stream ended.
func (s *SSEBlockSource) flushTail() ([]byte, error) {
	if len(s.pending) > 0 {
		block := make([]byte, len(s.pending))
		copy(block, s.pending)
		s.pending = nil
		return block, nil
	}
	return nil, io.EOF
}

// Close releases the underlying reader.
func (s *SSEBlockSource) Close() error {
	if s.reader != nil {
		return s.reader.Close()
	}
	return nil
}

// sseBlockEndIndex returns the end index (exclusive) of the first complete
// SSE block in b, or -1 when no block boundary is present yet.
func sseBlockEndIndex(b []byte) int {
	if i := bytes.Index(b, []byte("\n\n")); i >= 0 {
		return i + 2
	}
	if i := bytes.Index(b, []byte("\r\n\r\n")); i >= 0 {
		return i + 4
	}
	return -1
}

// SSEBlockDataPayload extracts the joined data payload of an SSE block
// (without the "data: " prefix), or nil when the block carries no data line
// (comment-only frames, handshakes). Multiple data: lines are joined with
// newlines per the SSE spec.
func SSEBlockDataPayload(block []byte) []byte {
	var payload []byte
	for _, line := range bytes.Split(block, []byte("\n")) {
		line = bytes.TrimSuffix(line, []byte("\r"))
		if len(line) == 0 {
			continue
		}
		if bytes.HasPrefix(line, []byte("data:")) {
			value := bytes.TrimPrefix(line[len("data:"):], []byte(" "))
			if len(payload) > 0 {
				payload = append(payload, '\n')
			}
			payload = append(payload, value...)
		}
	}
	if len(payload) == 0 {
		return nil
	}
	return payload
}