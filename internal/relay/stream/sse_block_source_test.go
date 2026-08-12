package stream

import (
	"bytes"
	"context"
	"errors"
	"io"
	"testing"
)

func readAllBlocks(t *testing.T, src *SSEBlockSource) []string {
	t.Helper()
	var blocks []string
	for {
		block, err := src.ReadEvent(context.Background())
		if errors.Is(err, io.EOF) {
			return blocks
		}
		if err != nil {
			t.Fatalf("ReadEvent error = %v", err)
		}
		blocks = append(blocks, string(block))
	}
}

func TestSSEBlockSourceSplitsBlocksPreservingBytes(t *testing.T) {
	raw := "data: {a}\n\ndata: {b}\n\ndata: {c}\n\n"
	src := NewSSEBlockSource(io.NopCloser(bytes.NewReader([]byte(raw))), 1024)
	blocks := readAllBlocks(t, src)
	want := []string{"data: {a}\n\n", "data: {b}\n\n", "data: {c}\n\n"}
	if len(blocks) != len(want) {
		t.Fatalf("expected %d blocks, got %d: %q", len(want), len(blocks), blocks)
	}
	for i := range want {
		if blocks[i] != want[i] {
			t.Fatalf("block[%d] = %q, want %q", i, blocks[i], want[i])
		}
	}
}

func TestSSEBlockSourceFlushesTrailingPartialBlock(t *testing.T) {
	raw := "data: {a}\n\ndata: {b}"
	src := NewSSEBlockSource(io.NopCloser(bytes.NewReader([]byte(raw))), 1024)
	blocks := readAllBlocks(t, src)
	want := []string{"data: {a}\n\n", "data: {b}"}
	if len(blocks) != len(want) {
		t.Fatalf("expected %d blocks, got %d: %q", len(want), len(blocks), blocks)
	}
	for i := range want {
		if blocks[i] != want[i] {
			t.Fatalf("block[%d] = %q, want %q", i, blocks[i], want[i])
		}
	}
}

// event: 行与注释帧必须逐字节保留（透传字节保真的一部分）。
func TestSSEBlockSourcePreservesEventLinesAndComments(t *testing.T) {
	raw := "event: response.custom_debug\ndata: {\"type\":\"response.custom_debug\"}\n\n: ping\n\ndata: {b}\n\n"
	src := NewSSEBlockSource(io.NopCloser(bytes.NewReader([]byte(raw))), 1024)
	blocks := readAllBlocks(t, src)
	want := []string{
		"event: response.custom_debug\ndata: {\"type\":\"response.custom_debug\"}\n\n",
		": ping\n\n",
		"data: {b}\n\n",
	}
	if len(blocks) != len(want) {
		t.Fatalf("expected %d blocks, got %d: %q", len(want), len(blocks), blocks)
	}
	for i := range want {
		if blocks[i] != want[i] {
			t.Fatalf("block[%d] = %q, want %q", i, blocks[i], want[i])
		}
	}
}

func TestSSEBlockSourceHandlesCRLF(t *testing.T) {
	raw := "data: {a}\r\n\r\ndata: {b}\r\n\r\n"
	src := NewSSEBlockSource(io.NopCloser(bytes.NewReader([]byte(raw))), 1024)
	blocks := readAllBlocks(t, src)
	want := []string{"data: {a}\r\n\r\n", "data: {b}\r\n\r\n"}
	if len(blocks) != len(want) {
		t.Fatalf("expected %d blocks, got %d: %q", len(want), len(blocks), blocks)
	}
	for i := range want {
		if blocks[i] != want[i] {
			t.Fatalf("block[%d] = %q, want %q", i, blocks[i], want[i])
		}
	}
}

func TestSSEBlockSourceRejectsOversizedBlock(t *testing.T) {
	raw := append([]byte("data: "), bytes.Repeat([]byte("x"), 64)...)
	raw = append(raw, '\n', '\n')
	src := NewSSEBlockSource(io.NopCloser(bytes.NewReader(raw)), 32)
	_, err := src.ReadEvent(context.Background())
	if !errors.Is(err, ErrSSEBlockTooLarge) {
		t.Fatalf("expected ErrSSEBlockTooLarge, got %v", err)
	}
}

func TestSSEBlockDataPayload(t *testing.T) {
	cases := []struct {
		block string
		want  string
	}{
		{`data: {"type":"x"}` + "\n\n", `{"type":"x"}`},
		{"event: x\ndata: {\"a\":1}\n\n", `{"a":1}`},
		{": ping\n\n", ""},
		{"data: a\ndata: b\n\n", "a\nb"},
		{`data:{"a":1}` + "\n\n", `{"a":1}`},
		{"data: \n\n", ""},
	}
	for _, c := range cases {
		got := SSEBlockDataPayload([]byte(c.block))
		if c.want == "" {
			if got != nil {
				t.Fatalf("SSEBlockDataPayload(%q) = %q, want nil", c.block, got)
			}
			continue
		}
		if string(got) != c.want {
			t.Fatalf("SSEBlockDataPayload(%q) = %q, want %q", c.block, got, c.want)
		}
	}
}
