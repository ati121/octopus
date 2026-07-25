package iolimit

import (
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"strconv"
	"strings"
)

const bytesPerMB int64 = 1024 * 1024

const DefaultErrorBodyMaxBytes int64 = 256 * 1024

const (
	EnvRequestBodyMaxMB      = "OCTOPUS_REQUEST_BODY_MAX_MB"
	EnvImportBodyMaxMB       = "OCTOPUS_IMPORT_BODY_MAX_MB"
	EnvUpstreamResponseMaxMB = "OCTOPUS_UPSTREAM_RESPONSE_MAX_MB"
	EnvMetadataResponseMaxMB = "OCTOPUS_METADATA_RESPONSE_MAX_MB"

	DefaultRequestBodyMaxMB      int64 = 32
	DefaultImportBodyMaxMB       int64 = 256
	DefaultUpstreamResponseMaxMB int64 = 64
	DefaultMetadataResponseMaxMB int64 = 32
)

// TooLargeError reports that a bounded read exceeded its configured limit.
type TooLargeError struct {
	Limit int64
}

func (e *TooLargeError) Error() string {
	return fmt.Sprintf("body exceeds maximum size of %d bytes", e.Limit)
}

func IsTooLarge(err error) bool {
	var tooLarge *TooLargeError
	if errors.As(err, &tooLarge) {
		return true
	}
	var maxErr *http.MaxBytesError
	return errors.As(err, &maxErr)
}

// ReadAll reads at most limit bytes. It reads one additional byte so an exact
// limit-sized payload is accepted while an oversized payload is rejected.
func ReadAll(r io.Reader, limit int64) ([]byte, error) {
	if r == nil {
		return nil, errors.New("nil reader")
	}
	if limit <= 0 {
		return nil, fmt.Errorf("invalid read limit: %d", limit)
	}

	body, err := io.ReadAll(io.LimitReader(r, limit+1))
	if err != nil {
		return nil, err
	}
	if int64(len(body)) > limit {
		return nil, &TooLargeError{Limit: limit}
	}
	return body, nil
}

// ReadAtMost returns the first limit bytes and reports whether additional data
// was present. It is intended for bounded diagnostics such as upstream errors.
func ReadAtMost(r io.Reader, limit int64) ([]byte, bool, error) {
	if r == nil {
		return nil, false, errors.New("nil reader")
	}
	if limit <= 0 {
		return nil, false, fmt.Errorf("invalid read limit: %d", limit)
	}
	body, err := io.ReadAll(io.LimitReader(r, limit+1))
	if err != nil {
		return nil, false, err
	}
	if int64(len(body)) > limit {
		return body[:limit], true, nil
	}
	return body, false, nil
}

// ReadRequestBody applies net/http's connection-aware request limit before
// reading. MaxBytesReader asks the server to close oversized request bodies
// instead of leaving an arbitrarily large unread body on a reusable connection.
func ReadRequestBody(w http.ResponseWriter, r *http.Request, limit int64) ([]byte, error) {
	if r == nil || r.Body == nil {
		return nil, errors.New("nil request body")
	}
	if limit <= 0 {
		return nil, fmt.Errorf("invalid request body limit: %d", limit)
	}

	r.Body = http.MaxBytesReader(w, r.Body, limit)
	body, err := io.ReadAll(r.Body)
	if err == nil {
		return body, nil
	}
	var maxErr *http.MaxBytesError
	if errors.As(err, &maxErr) {
		return nil, &TooLargeError{Limit: maxErr.Limit}
	}
	return nil, err
}

func RequestBodyMaxBytes() int64 {
	return envMegabytes(EnvRequestBodyMaxMB, DefaultRequestBodyMaxMB)
}

func ImportBodyMaxBytes() int64 {
	return envMegabytes(EnvImportBodyMaxMB, DefaultImportBodyMaxMB)
}

func UpstreamResponseMaxBytes() int64 {
	return envMegabytes(EnvUpstreamResponseMaxMB, DefaultUpstreamResponseMaxMB)
}

func MetadataResponseMaxBytes() int64 {
	return envMegabytes(EnvMetadataResponseMaxMB, DefaultMetadataResponseMaxMB)
}

func envMegabytes(name string, fallback int64) int64 {
	raw := strings.TrimSpace(os.Getenv(name))
	if raw == "" {
		return fallback * bytesPerMB
	}
	value, err := strconv.ParseInt(raw, 10, 64)
	if err != nil || value <= 0 || value > (1<<62)/bytesPerMB {
		return fallback * bytesPerMB
	}
	return value * bytesPerMB
}
