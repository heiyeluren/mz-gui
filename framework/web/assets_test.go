package webassets

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestTokenHandlerServesFrameworkDesignTokens(t *testing.T) {
	request := httptest.NewRequest(http.MethodGet, "/tokens.css", nil)
	response := httptest.NewRecorder()
	TokenHandler().ServeHTTP(response, request)
	if response.Code != http.StatusOK {
		t.Fatalf("读取 token 失败: %d", response.Code)
	}
	body := response.Body.String()
	for _, token := range []string{"--mzgui-color-surface", "--mzgui-radius-control", "--mzgui-motion-fast", "data-mzgui-theme='high-contrast'"} {
		if !strings.Contains(body, token) {
			t.Errorf("缺少框架设计变量 %s", token)
		}
	}
}
