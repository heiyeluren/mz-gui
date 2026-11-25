package fault

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"regexp"
)

var (
	ErrInvalidCatalog = errors.New("Framework 错误 registry 无效")
	releasePattern    = regexp.MustCompile(`^[1-9][0-9]*\.[0-9]+\.[0-9]+$`)
)

type CodeDefinition struct {
	Code     Code   `json:"code"`
	GRPCCode string `json:"grpc_code"`
}

type ReasonDefinition struct {
	ReasonCode  string `json:"reason_code"`
	Code        Code   `json:"code"`
	MessageKey  string `json:"message_key"`
	UserMessage string `json:"user_message"`
	Retryable   bool   `json:"retryable"`
}

type Catalog struct {
	SchemaRef     string             `json:"$schema,omitempty"`
	SchemaVersion int                `json:"schema_version"`
	Release       string             `json:"release"`
	Codes         []CodeDefinition   `json:"codes"`
	Reasons       []ReasonDefinition `json:"reasons"`
}

func DecodeCatalog(data []byte) (Catalog, error) {
	var catalog Catalog
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&catalog); err != nil {
		return Catalog{}, invalidCatalog("%v", err)
	}
	var extra any
	if err := decoder.Decode(&extra); !errors.Is(err, io.EOF) {
		return Catalog{}, invalidCatalog("存在多余 JSON 内容")
	}
	if catalog.SchemaVersion != 1 || !releasePattern.MatchString(catalog.Release) || len(catalog.Codes) == 0 || len(catalog.Reasons) == 0 {
		return Catalog{}, invalidCatalog("顶层字段无效")
	}
	codes := make(map[Code]CodeDefinition, len(catalog.Codes))
	for _, definition := range catalog.Codes {
		if !validCode(definition.Code) || definition.GRPCCode == "" {
			return Catalog{}, invalidCatalog("错误分类无效: %s", definition.Code)
		}
		if _, exists := codes[definition.Code]; exists {
			return Catalog{}, invalidCatalog("错误分类重复: %s", definition.Code)
		}
		codes[definition.Code] = definition
	}
	reasons := make(map[string]struct{}, len(catalog.Reasons))
	for _, definition := range catalog.Reasons {
		if !reasonPattern.MatchString(definition.ReasonCode) || !messageKeyPattern.MatchString(definition.MessageKey) ||
			!validPublicText(definition.UserMessage, 256) {
			return Catalog{}, invalidCatalog("reason 定义无效: %s", definition.ReasonCode)
		}
		if _, exists := codes[definition.Code]; !exists {
			return Catalog{}, invalidCatalog("reason 引用未声明分类: %s", definition.Code)
		}
		if _, exists := reasons[definition.ReasonCode]; exists {
			return Catalog{}, invalidCatalog("reason 重复: %s", definition.ReasonCode)
		}
		reasons[definition.ReasonCode] = struct{}{}
	}
	return catalog, nil
}

func (catalog Catalog) Code(code Code) (CodeDefinition, bool) {
	for _, definition := range catalog.Codes {
		if definition.Code == code {
			return definition, true
		}
	}
	return CodeDefinition{}, false
}

func (catalog Catalog) Reason(reasonCode string) (ReasonDefinition, bool) {
	for _, definition := range catalog.Reasons {
		if definition.ReasonCode == reasonCode {
			return definition, true
		}
	}
	return ReasonDefinition{}, false
}
