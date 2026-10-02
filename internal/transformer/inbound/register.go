package inbound

import (
	"github.com/bestruirui/octopus/internal/transformer/axon"
	"github.com/bestruirui/octopus/internal/transformer/model"
)

type InboundType int

const (
	InboundTypeOpenAIChat InboundType = iota
	InboundTypeOpenAIResponse
	InboundTypeAnthropic
	InboundTypeOpenAIEmbedding
	InboundTypeRerank
	InboundTypeSystemOne
)

var inboundFactories = map[InboundType]func() model.Inbound{
	InboundTypeOpenAIChat:      func() model.Inbound { return axon.NewInbound(0) },
	InboundTypeOpenAIResponse:  func() model.Inbound { return axon.NewInbound(1) },
	InboundTypeOpenAIEmbedding: func() model.Inbound { return axon.NewInbound(3) },
	InboundTypeAnthropic:       func() model.Inbound { return axon.NewInbound(2) },
	InboundTypeRerank:          func() model.Inbound { return axon.NewInbound(4) },
	InboundTypeSystemOne:       func() model.Inbound { return axon.NewInbound(5) },
}

func Get(inboundType InboundType) model.Inbound {
	if factory, ok := inboundFactories[inboundType]; ok {
		return factory()
	}
	return nil
}
