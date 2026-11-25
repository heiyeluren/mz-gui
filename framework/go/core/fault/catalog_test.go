package fault

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func TestFrameworkErrorCatalogIsValidAndComplete(t *testing.T) {
	root, err := filepath.Abs(filepath.Join("..", "..", "..", ".."))
	if err != nil {
		t.Fatal(err)
	}
	schema, err := os.ReadFile(filepath.Join(root, "framework", "errors", "v1.schema.json"))
	if err != nil || !json.Valid(schema) {
		t.Fatalf("错误 registry schema 无效: %v", err)
	}
	data, err := os.ReadFile(filepath.Join(root, "framework", "errors", "v1.json"))
	if err != nil {
		t.Fatal(err)
	}
	catalog, err := DecodeCatalog(data)
	if err != nil {
		t.Fatal(err)
	}
	if catalog.SchemaRef != "./v1.schema.json" || len(catalog.Codes) != len(AllCodes()) || len(catalog.Reasons) != 5 {
		t.Fatalf("错误 registry 基线异常: %#v", catalog)
	}
	for _, code := range AllCodes() {
		if _, ok := catalog.Code(code); !ok {
			t.Errorf("错误 registry 缺少分类 %s", code)
		}
	}
	for _, reason := range []string{"core.operation_cancelled", "core.operation_timeout", "core.internal_panic"} {
		if _, ok := catalog.Reason(reason); !ok {
			t.Errorf("错误 registry 缺少 reason %s", reason)
		}
	}
}

func TestDecodeCatalogRejectsUnknownAndDuplicateEntries(t *testing.T) {
	valid := `{"schema_version":1,"release":"1.0.0","codes":[{"code":"INTERNAL","grpc_code":"Internal"}],"reasons":[{"reason_code":"core.unknown_internal","code":"INTERNAL","message_key":"errors.internal","user_message":"内部错误","retryable":false}]}`
	cases := []string{
		`{"schema_version":1,"release":"1.0.0","unknown":true,"codes":[],"reasons":[]}`,
		`{"schema_version":1,"release":"1.0.0","codes":[{"code":"INTERNAL","grpc_code":"Internal"},{"code":"INTERNAL","grpc_code":"Internal"}],"reasons":[{"reason_code":"core.unknown_internal","code":"INTERNAL","message_key":"errors.internal","user_message":"内部错误","retryable":false}]}`,
		`{"schema_version":1,"release":"1.0.0","codes":[{"code":"INTERNAL","grpc_code":"Internal"}],"reasons":[{"reason_code":"core.unknown_internal","code":"NOT_FOUND","message_key":"errors.internal","user_message":"内部错误","retryable":false}]}`,
	}
	if _, err := DecodeCatalog([]byte(valid)); err != nil {
		t.Fatalf("合法最小 registry 应通过: %v", err)
	}
	for index, data := range cases {
		if _, err := DecodeCatalog([]byte(data)); !errors.Is(err, ErrInvalidCatalog) {
			t.Fatalf("非法 registry %d 应被拒绝，实际为 %v", index, err)
		}
	}
}
