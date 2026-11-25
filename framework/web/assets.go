package webassets

import (
	"embed"
	"net/http"
)

//go:embed tokens.css
var files embed.FS

// TokenHandler 为示例和应用提供同一份框架设计变量。
func TokenHandler() http.Handler {
	return http.FileServer(http.FS(files))
}
