package relay

import (
	"bytes"
	"image"
	"image/png"
	"io"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"net/textproto"
	"strings"
	"testing"

	"github.com/bestruirui/octopus/internal/model"
	"github.com/bestruirui/octopus/internal/op"
	"github.com/bestruirui/octopus/internal/transformer/outbound"
	"github.com/gin-gonic/gin"
	"github.com/tidwall/gjson"
)

func TestAxonHubImagesHandlers(t *testing.T) {
	gin.SetMode(gin.TestMode)
	ctx := setupRelayTestDB(t)
	var pngData bytes.Buffer
	if err := png.Encode(&pngData, image.NewRGBA(image.Rect(0, 0, 1, 1))); err != nil {
		t.Fatal(err)
	}
	for _, endpoint := range []string{"/images/generations", "/images/edits", "/images/variations"} {
		t.Run(endpoint, func(t *testing.T) {
			called := 0
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				called++
				if r.URL.Path != "/v1"+endpoint || r.Header.Get("Authorization") != "Bearer upstream-key" {
					t.Errorf("invalid image routing/auth: %s", r.URL.Path)
				}
				if endpoint == "/images/generations" {
					body, _ := io.ReadAll(r.Body)
					if gjson.GetBytes(body, "model").String() != "image-native" || gjson.GetBytes(body, "prompt").String() != "tree" {
						t.Errorf("image input lost: %s", body)
					}
				} else {
					if err := r.ParseMultipartForm(1 << 20); err != nil {
						t.Error(err)
						return
					}
					defer r.MultipartForm.RemoveAll()
					if r.FormValue("model") != "image-native" {
						t.Error("routed model lost")
					}
					file, _, err := r.FormFile("image")
					if err != nil {
						t.Error(err)
						return
					}
					body, _ := io.ReadAll(file)
					_ = file.Close()
					if !bytes.Equal(body, pngData.Bytes()) {
						t.Error("image data lost")
					}
				}
				w.Header().Set("Content-Type", "application/json")
				_, _ = io.WriteString(w, `{"created":1,"data":[{"b64_json":"aGVsbG8="}],"usage":{"input_tokens":2,"output_tokens":3,"total_tokens":5}}`)
			}))
			defer server.Close()
			groupName := "axon-" + strings.TrimPrefix(endpoint, "/images/")
			channel := &model.Channel{Name: groupName, Type: outbound.OutboundTypeOpenAIChat, Enabled: true, Model: "image-native", BaseUrls: []model.BaseUrl{{URL: server.URL + "/v1"}}, Keys: []model.ChannelKey{{Enabled: true, ChannelKey: "upstream-key"}}}
			if err := op.ChannelCreate(channel, ctx); err != nil {
				t.Fatal(err)
			}
			createSystemOneTestGroup(t, groupName, channel.ID, "image-native")
			body := []byte(`{"model":"` + groupName + `","prompt":"tree"}`)
			contentType := "application/json"
			if endpoint != "/images/generations" {
				var b bytes.Buffer
				mw := multipart.NewWriter(&b)
				_ = mw.WriteField("model", groupName)
				if endpoint == "/images/edits" {
					_ = mw.WriteField("prompt", "tree")
				}
				f, err := mw.CreatePart(textproto.MIMEHeader{"Content-Disposition": {`form-data; name="image"; filename="test.png"`}, "Content-Type": {"image/png"}})
				if err != nil {
					t.Fatal(err)
				}
				_, _ = f.Write(pngData.Bytes())
				_ = mw.Close()
				body = b.Bytes()
				contentType = mw.FormDataContentType()
			}
			recorder := httptest.NewRecorder()
			c, _ := gin.CreateTestContext(recorder)
			c.Request = httptest.NewRequest("POST", "/v1"+endpoint, bytes.NewReader(body))
			c.Request.Header.Set("Content-Type", contentType)
			c.Request.Header.Set("Authorization", "Bearer client-key")
			ImagesHandler(endpoint, c)
			if called != 1 || recorder.Code != 200 || gjson.GetBytes(recorder.Body.Bytes(), "data.0.b64_json").String() != "aGVsbG8=" {
				t.Fatalf("images failed: calls=%d status=%d body=%s", called, recorder.Code, recorder.Body.String())
			}
			logs, err := op.RelayLogList(ctx, nil, nil, nil, 1, 20)
			if err != nil {
				t.Fatal(err)
			}
			for _, entry := range logs {
				if entry.RequestModelName == groupName {
					if !entry.Success || entry.InputTokens != 2 || entry.OutputTokens != 3 {
						t.Errorf("image usage lost: %+v", entry)
					}
					return
				}
			}
			t.Fatal("missing image log")
		})
	}
}

func TestAxonHubCompactHandler(t *testing.T) {
	gin.SetMode(gin.TestMode)
	ctx := setupRelayTestDB(t)
	called := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		called++
		body, _ := io.ReadAll(r.Body)
		if r.URL.Path != "/v1/responses/compact" || r.Header.Get("Authorization") != "Bearer upstream-key" || gjson.GetBytes(body, "model").String() != "compact-native" {
			t.Errorf("compact routing/auth failed: %s %s", r.URL.Path, body)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"id":"cmp_1","object":"response.compaction","created_at":1,"output":[{"type":"compaction","id":"item_1","encrypted_content":"test-encrypted"}],"usage":{"input_tokens":7,"output_tokens":2,"total_tokens":9}}`)
	}))
	defer server.Close()
	channel := &model.Channel{Name: "axon-compact", Type: outbound.OutboundTypeOpenAIResponse, Enabled: true, Model: "compact-native", BaseUrls: []model.BaseUrl{{URL: server.URL + "/v1"}}, Keys: []model.ChannelKey{{Enabled: true, ChannelKey: "upstream-key"}}}
	if err := op.ChannelCreate(channel, ctx); err != nil {
		t.Fatal(err)
	}
	createSystemOneTestGroup(t, "axon-compact", channel.ID, "compact-native")
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest("POST", "/v1/responses/compact", strings.NewReader(`{"model":"axon-compact","input":[{"type":"message","role":"user","content":"hello"}]}`))
	c.Request.Header.Set("Content-Type", "application/json")
	c.Request.Header.Set("Authorization", "Bearer client-key")
	HandleResponsesCompact(c)
	if called != 1 || recorder.Code != 200 || gjson.GetBytes(recorder.Body.Bytes(), "output.0.encrypted_content").String() != "test-encrypted" {
		t.Fatalf("compact failed: calls=%d status=%d body=%s", called, recorder.Code, recorder.Body.String())
	}
	logs, err := op.RelayLogList(ctx, nil, nil, nil, 1, 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(logs) == 0 || logs[0].RequestModelName != "axon-compact" || !logs[0].Success || logs[0].InputTokens != 7 || logs[0].OutputTokens != 2 || logs[0].ActualModelName != "compact-native" {
		t.Fatalf("compact metrics lost: %+v", logs)
	}
}
