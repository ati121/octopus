package iolimit

import (
	"errors"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestReadAllAcceptsExactLimit(t *testing.T) {
	body, err := ReadAll(strings.NewReader("1234"), 4)
	if err != nil {
		t.Fatalf("ReadAll returned error: %v", err)
	}
	if string(body) != "1234" {
		t.Fatalf("unexpected body %q", body)
	}
}

func TestReadAllRejectsOversizedBody(t *testing.T) {
	_, err := ReadAll(strings.NewReader("12345"), 4)
	var tooLarge *TooLargeError
	if !errors.As(err, &tooLarge) || tooLarge.Limit != 4 {
		t.Fatalf("expected TooLargeError(limit=4), got %v", err)
	}
}

func TestReadRequestBodyRejectsOversizedBody(t *testing.T) {
	req := httptest.NewRequest("POST", "/", strings.NewReader("12345"))
	w := httptest.NewRecorder()
	_, err := ReadRequestBody(w, req, 4)
	if !IsTooLarge(err) {
		t.Fatalf("expected oversized request error, got %v", err)
	}
}

func TestReadAtMostReportsTruncation(t *testing.T) {
	body, truncated, err := ReadAtMost(strings.NewReader("12345"), 4)
	if err != nil {
		t.Fatalf("ReadAtMost returned error: %v", err)
	}
	if !truncated || string(body) != "1234" {
		t.Fatalf("unexpected result body=%q truncated=%t", body, truncated)
	}
}

func TestRequestBodyMaxBytesFromEnvironment(t *testing.T) {
	t.Setenv(EnvRequestBodyMaxMB, "7")
	if got := RequestBodyMaxBytes(); got != 7*bytesPerMB {
		t.Fatalf("unexpected request limit: %d", got)
	}
}
