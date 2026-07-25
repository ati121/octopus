package relay

import (
	"strings"

	"github.com/bestruirui/octopus/internal/model"
)

func apiKeyAllowsModel(supportedModelsCSV, mode, requestModel string) bool {
	key := model.APIKey{
		SupportedModels: supportedModelsCSV,
		ModelListMode:   mode,
	}
	return key.ModelAllowed(strings.TrimSpace(requestModel))
}
