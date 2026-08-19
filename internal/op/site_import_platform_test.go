package op

import (
	"testing"

	"github.com/bestruirui/octopus/internal/model"
)

func TestImportPlatformRecognizesSiliconFlowAsDirectProvider(t *testing.T) {
	for _, input := range []struct {
		platform any
		url      string
	}{
		{platform: "siliconflow", url: "https://api.siliconflow.cn"},
		{platform: "openai-compatible", url: "https://api.siliconflow.cn/v1"},
	} {
		platform, ok := resolveImportedPlatform(input.platform, input.url)
		if !ok || platform != model.SitePlatformSiliconFlow {
			t.Fatalf("expected SiliconFlow import platform for %#v, got %q ok=%v", input, platform, ok)
		}
	}
	if !isDirectImportPlatform(model.SitePlatformSiliconFlow) {
		t.Fatal("expected SiliconFlow to use direct-provider account import behavior")
	}
	if platformSupportsCheckin(model.SitePlatformSiliconFlow) {
		t.Fatal("expected SiliconFlow imports not to enable managed-platform check-in")
	}
}

func TestImportUnknownPlatformFallsBackToOther(t *testing.T) {
	for _, input := range []struct {
		platform any
		url      string
	}{
		{platform: "sensenova", url: "https://token.sensenova.cn"},
		{platform: "", url: "https://custom-provider.example.com"},
		{platform: "other", url: "https://custom-provider.example.com"},
	} {
		platform, ok := resolveImportedPlatform(input.platform, input.url)
		if !ok || platform != model.SitePlatformOther {
			t.Fatalf("expected Other import platform for %#v, got %q ok=%v", input, platform, ok)
		}
	}

	profilePlatform, ok := resolveImportedProfilePlatform("openai-compatible", "https://token.sensenova.cn/v1")
	if !ok || profilePlatform != model.SitePlatformOther {
		t.Fatalf("expected unknown compatible profile to import as Other, got %q ok=%v", profilePlatform, ok)
	}
	if !isDirectImportPlatform(model.SitePlatformOther) {
		t.Fatal("expected Other to use direct-provider account import behavior")
	}
	if platformSupportsCheckin(model.SitePlatformOther) {
		t.Fatal("expected Other imports not to enable managed-platform check-in")
	}
}

func TestImportKnownPlatformAliasesDoNotFallBackToOther(t *testing.T) {
	tests := []struct {
		platform any
		expected model.SitePlatform
	}{
		{platform: "New API", expected: model.SitePlatformNewAPI},
		{platform: "Any Router", expected: model.SitePlatformAnyRouter},
		{platform: "One API", expected: model.SitePlatformOneAPI},
		{platform: "One Hub", expected: model.SitePlatformOneHub},
		{platform: "Done Hub", expected: model.SitePlatformDoneHub},
		{platform: "Silicon Flow", expected: model.SitePlatformSiliconFlow},
	}

	for _, tt := range tests {
		platform, ok := resolveImportedPlatform(tt.platform, "https://provider.example.com")
		if !ok || platform != tt.expected {
			t.Fatalf("expected %q for alias %q, got %q ok=%v", tt.expected, tt.platform, platform, ok)
		}
	}
}
