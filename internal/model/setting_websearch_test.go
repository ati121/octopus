package model

import "testing"

func TestWebSearchMaxRoundsValidation(t *testing.T) {
	t.Parallel()

	tests := []struct {
		value   string
		wantErr bool
	}{
		{value: "1"},
		{value: "10"},
		{value: "100"},
		{value: "0", wantErr: true},
		{value: "-1", wantErr: true},
		{value: "101", wantErr: true},
		{value: "1.5", wantErr: true},
		{value: "invalid", wantErr: true},
	}

	for _, tt := range tests {
		t.Run(tt.value, func(t *testing.T) {
			setting := Setting{Key: SettingKeyWebSearchMaxRounds, Value: tt.value}
			err := setting.Validate()
			if (err != nil) != tt.wantErr {
				t.Fatalf("Validate() error = %v, wantErr %t", err, tt.wantErr)
			}
		})
	}
}

func TestDefaultWebSearchMaxRounds(t *testing.T) {
	t.Parallel()

	for _, setting := range DefaultSettings() {
		if setting.Key == SettingKeyWebSearchMaxRounds {
			if setting.Value != "10" {
				t.Fatalf("default web search max rounds = %q, want 10", setting.Value)
			}
			return
		}
	}
	t.Fatal("default web search max rounds setting is missing")
}
