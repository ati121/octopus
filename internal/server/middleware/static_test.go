package middleware

import (
	"bytes"
	"compress/gzip"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
	"testing/fstest"

	"github.com/gin-gonic/gin"
)

func gzipBytes(t *testing.T, data string) []byte {
	t.Helper()
	var buf bytes.Buffer
	w := gzip.NewWriter(&buf)
	if _, err := w.Write([]byte(data)); err != nil {
		t.Fatalf("gzip write: %v", err)
	}
	if err := w.Close(); err != nil {
		t.Fatalf("gzip close: %v", err)
	}
	return buf.Bytes()
}

func newStaticTestRouter(t *testing.T) *gin.Engine {
	t.Helper()
	gin.SetMode(gin.TestMode)
	mapFS := fstest.MapFS{
		"index.html":                       {Data: []byte("<html>hello</html>")},
		"index.html.gz":                    {Data: gzipBytes(t, "<html>hello</html>")},
		"_next/static/chunks/abc123.js":    {Data: []byte("console.log(1)")},
		"_next/static/chunks/abc123.js.gz": {Data: gzipBytes(t, "console.log(1)")},
		"_next/static/chunks/plain.css":    {Data: []byte("body{}")},
		"manifest.json":                    {Data: []byte(`{"name":"octopus"}`)},
		"manifest.json.gz":                 {Data: gzipBytes(t, `{"name":"octopus"}`)},
	}
	r := gin.New()
	r.Use(StaticEmbed("/", mapFS))
	r.NoRoute(func(c *gin.Context) {
		c.Status(http.StatusNotFound)
	})
	return r
}

func TestStaticServesPrecompressedGzip(t *testing.T) {
	r := newStaticTestRouter(t)

	req := httptest.NewRequest(http.MethodGet, "/index.html", nil)
	req.Header.Set("Accept-Encoding", "gzip")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", w.Code)
	}
	if got := w.Header().Get("Content-Encoding"); got != "gzip" {
		t.Fatalf("Content-Encoding = %q, want gzip", got)
	}
	if got := w.Header().Get("Cache-Control"); got != "no-cache" {
		t.Fatalf("Cache-Control = %q, want no-cache", got)
	}
	if got := w.Header().Get("Content-Type"); got != "text/html; charset=utf-8" {
		t.Fatalf("Content-Type = %q, want text/html; charset=utf-8", got)
	}
	zr, err := gzip.NewReader(w.Body)
	if err != nil {
		t.Fatalf("gzip reader: %v", err)
	}
	body, err := io.ReadAll(zr)
	if err != nil {
		t.Fatalf("gunzip: %v", err)
	}
	if string(body) != "<html>hello</html>" {
		t.Fatalf("body = %q", body)
	}
}

func TestStaticHashedAssetPrecompressedImmutable(t *testing.T) {
	r := newStaticTestRouter(t)

	req := httptest.NewRequest(http.MethodGet, "/_next/static/chunks/abc123.js", nil)
	req.Header.Set("Accept-Encoding", "gzip")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", w.Code)
	}
	if got := w.Header().Get("Cache-Control"); got != "public, max-age=31536000, immutable" {
		t.Fatalf("Cache-Control = %q", got)
	}
	if got := w.Header().Get("Content-Encoding"); got != "gzip" {
		t.Fatalf("Content-Encoding = %q, want gzip", got)
	}
	if got := w.Header().Get("Content-Type"); got != "text/javascript; charset=utf-8" {
		t.Fatalf("Content-Type = %q, want text/javascript; charset=utf-8", got)
	}
}

func TestStaticIdentityWithoutAcceptEncoding(t *testing.T) {
	r := newStaticTestRouter(t)

	// 不带 Accept-Encoding 时必须回退原文件（/index.html 会走 FileServer 的
	// 规范化重定向，用 manifest.json 验证 identity 路径）。
	req := httptest.NewRequest(http.MethodGet, "/manifest.json", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", w.Code)
	}
	if got := w.Header().Get("Content-Encoding"); got != "" {
		t.Fatalf("Content-Encoding = %q, want empty", got)
	}
	if w.Body.String() != `{"name":"octopus"}` {
		t.Fatalf("body = %q", w.Body.String())
	}
}

func TestStaticFallbacksWhenNoGzipVariant(t *testing.T) {
	r := newStaticTestRouter(t)

	// plain.css 无 .gz 变体：带 Accept-Encoding 也要回退原文件。
	req := httptest.NewRequest(http.MethodGet, "/_next/static/chunks/plain.css", nil)
	req.Header.Set("Accept-Encoding", "gzip")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", w.Code)
	}
	if got := w.Header().Get("Content-Encoding"); got != "" {
		t.Fatalf("Content-Encoding = %q, want empty", got)
	}
	if w.Body.String() != "body{}" {
		t.Fatalf("body = %q", w.Body.String())
	}
}

func TestStaticSkipsApiAndMissingPaths(t *testing.T) {
	r := newStaticTestRouter(t)

	for _, target := range []string{"/api/v1/channel", "/missing.js"} {
		req := httptest.NewRequest(http.MethodGet, target, nil)
		req.Header.Set("Accept-Encoding", "gzip")
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)
		if w.Code != http.StatusNotFound {
			t.Fatalf("%s: status = %d, want 404 (fall through)", target, w.Code)
		}
	}
}

func TestStaticHEADPrecompressed(t *testing.T) {
	r := newStaticTestRouter(t)

	req := httptest.NewRequest(http.MethodHead, "/manifest.json", nil)
	req.Header.Set("Accept-Encoding", "gzip")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", w.Code)
	}
	if got := w.Header().Get("Content-Encoding"); got != "gzip" {
		t.Fatalf("Content-Encoding = %q, want gzip", got)
	}
	// HEAD 的 body 丢弃由真实 net/http 服务器完成；recorder 只校验头一致。
	if got := w.Header().Get("Content-Length"); got == "" {
		t.Fatalf("Content-Length header missing")
	}
}
