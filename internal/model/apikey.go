package model

import "strings"

type APIKey struct {
	ID       int     `json:"id" gorm:"primaryKey"`
	Name     string  `json:"name" gorm:"not null"`
	APIKey   string  `json:"api_key" gorm:"not null"`
	Enabled  bool    `json:"enabled" gorm:"default:true"`
	ExpireAt int64   `json:"expire_at,omitempty"`
	MaxCost  float64 `json:"max_cost,omitempty"`
	MaxRPM   int     `json:"max_rpm,omitempty"`
	// SupportedModels is a comma-separated model list. Empty means unrestricted.
	SupportedModels string `json:"supported_models,omitempty"`
	// ModelListMode is "allow" (default/legacy whitelist) or "deny" (blacklist).
	ModelListMode string `json:"model_list_mode,omitempty" gorm:"type:varchar(16);not null;default:''"`
}

// ModelAllowed reports whether modelName is permitted by this API key's model-list policy.
func (k *APIKey) ModelAllowed(modelName string) bool {
	if k == nil {
		return true
	}
	models := splitCommaModels(k.SupportedModels)
	if len(models) == 0 {
		return true
	}

	modelName = strings.TrimSpace(modelName)
	inList := false
	for _, candidate := range models {
		if candidate == modelName {
			inList = true
			break
		}
	}
	if strings.EqualFold(strings.TrimSpace(k.ModelListMode), "deny") {
		return !inList
	}
	return inList
}

func splitCommaModels(value string) []string {
	if strings.TrimSpace(value) == "" {
		return nil
	}
	parts := strings.Split(value, ",")
	models := make([]string, 0, len(parts))
	for _, part := range parts {
		if modelName := strings.TrimSpace(part); modelName != "" {
			models = append(models, modelName)
		}
	}
	return models
}
