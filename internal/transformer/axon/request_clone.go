package axon

import (
	"maps"

	"github.com/looplj/axonhub/llm"
)

// Transformers read protocol payloads; only per-attempt transport/option maps are mutable.
func cloneNativeRequest(src *llm.Request) *llm.Request {
	value := *src
	if src.Stream != nil {
		v := *src.Stream
		value.Stream = &v
	}
	if src.PreviousResponseID != nil {
		v := *src.PreviousResponseID
		value.PreviousResponseID = &v
	}
	value.TransformerMetadata = maps.Clone(src.TransformerMetadata)
	if src.StreamOptions != nil {
		options := *src.StreamOptions
		value.StreamOptions = &options
	}
	if src.RawRequest != nil {
		raw := *src.RawRequest
		raw.Headers = src.RawRequest.Headers.Clone()
		raw.Query = cloneRequestQuery(src.RawRequest.Query)
		value.RawRequest = &raw
	}
	return &value
}

func cloneRequestQuery(src map[string][]string) map[string][]string {
	if src == nil {
		return nil
	}
	dst := make(map[string][]string, len(src))
	for key, values := range src {
		dst[key] = append([]string(nil), values...)
	}
	return dst
}
