package systemone

import (
	"bytes"
	"context"
	"strings"
	"testing"

	"github.com/bestruirui/octopus/internal/transformer/model"
)

func TestTransformRequestPreservesPayloadAndValidatesRequiredFields(t *testing.T) {
	body := []byte(`{"model":"  jev-latest  ","state":"Help! My payouts have been failing for 3 days.","questions":{"department":{"type":"choice","instructions":"Which team should handle this?","criteria":{"billing":"Payments","technical":null}},"frustration":{"type":"score","instructions":"How frustrated?","criteria":["Calm","Frustrated","Very angry"]},"is_urgent":{"type":"noul","instructions":"Does this convey urgency?"}},"vendor_extension":{"trace":"abc"}}`)

	request, err := (&Inbound{}).TransformRequest(context.Background(), body)
	if err != nil {
		t.Fatalf("TransformRequest returned error: %v", err)
	}
	if request.Model != "jev-latest" {
		t.Fatalf("expected trimmed model, got %q", request.Model)
	}
	if request.RawAPIFormat != model.APIFormatSystemOne {
		t.Fatalf("expected system one API format, got %q", request.RawAPIFormat)
	}
	if !bytes.Equal(request.SystemOnePayload, body) {
		t.Fatalf("expected raw system one payload to be preserved, got %s", request.SystemOnePayload)
	}
	if !request.IsSystemOneRequest() {
		t.Fatal("expected system one request")
	}
	if err := request.Validate(); err != nil {
		t.Fatalf("expected transformed request to validate, got %v", err)
	}
}

func TestTransformRequestAcceptsStructuredAndEmptyState(t *testing.T) {
	for _, state := range []string{`{"subject":"Duplicate charge","message":"Please help."}`, `[{"role":"user","text":"hi"}]`, `""`} {
		t.Run(state, func(t *testing.T) {
			body := `{"model":"jev-latest","state":` + state + `,"questions":{"q":{"type":"noul","instructions":{"question":"Is this about billing?"}}}}`
			if _, err := (&Inbound{}).TransformRequest(context.Background(), []byte(body)); err != nil {
				t.Fatalf("expected state %s to be accepted, got %v", state, err)
			}
		})
	}
}

func TestTransformRequestRejectsMissingRequiredFields(t *testing.T) {
	questions := `"questions":{"q":{"type":"noul","instructions":"i"}}`
	tests := []struct {
		name    string
		body    string
		message string
	}{
		{name: "invalid json", body: `{"model":`, message: "invalid system one request"},
		{name: "missing model", body: `{"state":"s",` + questions + `}`, message: "model is required"},
		{name: "blank model", body: `{"model":"  ","state":"s",` + questions + `}`, message: "model is required"},
		{name: "missing state", body: `{"model":"m",` + questions + `}`, message: "state is required"},
		{name: "null state", body: `{"model":"m","state":null,` + questions + `}`, message: "state is required"},
		{name: "missing questions", body: `{"model":"m","state":"s"}`, message: "questions must be"},
		{name: "null questions", body: `{"model":"m","state":"s","questions":null}`, message: "questions must be"},
		{name: "empty questions", body: `{"model":"m","state":"s","questions":{}}`, message: "questions must be"},
		{name: "questions array", body: `{"model":"m","state":"s","questions":[{"type":"noul"}]}`, message: "questions must be"},
		{name: "non object question", body: `{"model":"m","state":"s","questions":{"q":"Is it urgent?"}}`, message: "questions must be"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := (&Inbound{}).TransformRequest(context.Background(), []byte(tt.body))
			if err == nil || !strings.Contains(err.Error(), tt.message) {
				t.Fatalf("expected error containing %q, got %v", tt.message, err)
			}
		})
	}
}

func TestTransformResponseReturnsProviderPayloadUnchanged(t *testing.T) {
	raw := []byte(`{"model":"jev-1.13.0","answers":{"is_urgent":{"type":"noul","noul":0.95}},"usage":{"input_tokens":296,"output_tokens":20}}`)
	response := &model.InternalLLMResponse{Model: "jev-1.13.0", SystemOnePayload: raw}
	inbound := &Inbound{}

	body, err := inbound.TransformResponse(context.Background(), response)
	if err != nil {
		t.Fatalf("TransformResponse returned error: %v", err)
	}
	if !bytes.Equal(body, raw) {
		t.Fatalf("expected response payload to be preserved, got %s", body)
	}
	stored, err := inbound.GetInternalResponse(context.Background())
	if err != nil {
		t.Fatalf("GetInternalResponse returned error: %v", err)
	}
	if stored != response {
		t.Fatal("expected inbound adapter to retain the internal response")
	}

	body[0] = '['
	if response.SystemOnePayload[0] != '{' {
		t.Fatal("expected returned response bytes to be a copy")
	}
}

func TestTransformResponseRejectsEmptyResponse(t *testing.T) {
	for _, response := range []*model.InternalLLMResponse{nil, {}, {RerankPayload: []byte(`{"results":[]}`)}} {
		if _, err := (&Inbound{}).TransformResponse(context.Background(), response); err == nil {
			t.Fatal("expected non system one response to be rejected")
		}
	}
}

func TestTransformStreamIsUnsupported(t *testing.T) {
	if _, err := (&Inbound{}).TransformStream(context.Background(), &model.InternalLLMResponse{}); err == nil {
		t.Fatal("expected streaming to be unsupported")
	}
}
