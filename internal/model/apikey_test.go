package model

import "testing"

func TestAPIKeyModelAllowedAllowList(t *testing.T) {
	key := &APIKey{SupportedModels: " gpt-4o, claude-3 ", ModelListMode: "allow"}
	if !key.ModelAllowed("gpt-4o") || key.ModelAllowed("gpt-4.1") {
		t.Fatal("allow-list policy returned an unexpected result")
	}
	if !(&APIKey{}).ModelAllowed("anything") {
		t.Fatal("an empty allow list should allow every model")
	}
}

func TestAPIKeyModelAllowedDenyList(t *testing.T) {
	key := &APIKey{SupportedModels: "gpt-4o", ModelListMode: "deny"}
	if key.ModelAllowed("gpt-4o") || !key.ModelAllowed("claude-3") {
		t.Fatal("deny-list policy returned an unexpected result")
	}
	if !(&APIKey{ModelListMode: "deny"}).ModelAllowed("gpt-4o") {
		t.Fatal("an empty deny list should allow every model")
	}
}
