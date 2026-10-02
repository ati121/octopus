package axon

import (
	"fmt"
	"github.com/looplj/axonhub/llm/transformer"
	"github.com/looplj/axonhub/llm/transformer/openai"
)

func ImageInbound(endpoint string) (transformer.Inbound, error) {
	switch endpoint {
	case "/images/generations":
		return openai.NewImageGenerationInboundTransformer(), nil
	case "/images/edits":
		return openai.NewImageEditInboundTransformer(), nil
	case "/images/variations":
		return openai.NewImageVariationInboundTransformer(), nil
	default:
		return nil, fmt.Errorf("unsupported image endpoint %s", endpoint)
	}
}
