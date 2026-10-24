package middleware

import (
	"io"
	"io/fs"
	"mime"
	"net/http"
	"path"
	"strconv"
	"strings"

	"github.com/gin-gonic/gin"
)

// isHashedAsset checks if a path points to a content-hashed static asset
// that can be safely cached with immutable headers (e.g. _next/static/chunks/abc123.js).
func isHashedAsset(path string) bool {
	return strings.Contains(path, "/_next/static/") ||
		strings.Contains(path, "/static/chunks/") ||
		strings.Contains(path, "/static/css/") ||
		strings.Contains(path, "/static/media/")
}

func StaticEmbed(urlPrefix string, embedFS fs.FS) gin.HandlerFunc {
	fs := http.FS(embedFS)
	return static(urlPrefix, fs)
}

func StaticLocal(urlPrefix string, localPath string) gin.HandlerFunc {
	fs := http.Dir(localPath)
	return static(urlPrefix, fs)
}

func static(urlPrefix string, fileSystem http.FileSystem) gin.HandlerFunc {
	fileserver := http.FileServer(fileSystem)
	if urlPrefix != "" {
		fileserver = http.StripPrefix(urlPrefix, fileserver)
	}
	return func(c *gin.Context) {
		if strings.HasPrefix(c.Request.URL.Path, "/api") {
			c.Next()
			return
		}
		if _, err := fileSystem.Open(c.Request.URL.Path); err == nil {
			// 构建期生成的 .gz 预压缩变体优先，省去每请求动态压缩；
			// 这些扩展名已从 gin gzip 中间件排除，响应头不会被改写。
			if servePrecompressed(c, fileSystem) {
				c.Abort()
				return
			}
			if isHashedAsset(c.Request.URL.Path) {
				c.Header("Cache-Control", "public, max-age=31536000, immutable")
			} else {
				c.Header("Cache-Control", "no-cache")
			}
			fileserver.ServeHTTP(c.Writer, c.Request)
			c.Abort()
		}
	}
}

// servePrecompressed 在客户端接受 gzip 且存在 path+".gz" 时返回预压缩内容。
// 返回 false 表示未处理（客户端不接受 gzip 或没有对应变体），由调用方回退原文件。
func servePrecompressed(c *gin.Context, fileSystem http.FileSystem) bool {
	if !strings.Contains(c.Request.Header.Get("Accept-Encoding"), "gzip") {
		return false
	}
	gzPath := c.Request.URL.Path + ".gz"
	gzFile, err := fileSystem.Open(gzPath)
	if err != nil {
		return false
	}
	defer gzFile.Close()
	data, err := io.ReadAll(gzFile)
	if err != nil {
		return false
	}
	// Content-Type 按原始扩展名推断，而不是 .gz。
	contentType := mime.TypeByExtension(path.Ext(c.Request.URL.Path))
	if contentType == "" {
		contentType = "application/octet-stream"
	}
	if isHashedAsset(c.Request.URL.Path) {
		c.Header("Cache-Control", "public, max-age=31536000, immutable")
	} else {
		c.Header("Cache-Control", "no-cache")
	}
	c.Header("Content-Encoding", "gzip")
	c.Header("Vary", "Accept-Encoding")
	c.Header("Content-Length", strconv.Itoa(len(data)))
	c.Data(http.StatusOK, contentType, data)
	return true
}
