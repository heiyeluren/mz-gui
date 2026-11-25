package main

import (
	"errors"

	"github.com/heiyeluren/mz-gui/framework/go/core/fault"
)

var errShowcasePreviewSentinel = errors.New("showcase preview unavailable")

const showcaseFaultCatalogJSON = `{"schema_version":1,"release":"1.0.0","codes":[{"code":"UNAVAILABLE","grpc_code":"Unavailable"},{"code":"INVALID_ARGUMENT","grpc_code":"InvalidArgument"}],"reasons":[{"reason_code":"showcase.preview_unavailable","code":"UNAVAILABLE","message_key":"errors.preview_unavailable","user_message":"示例服务暂时不可用","retryable":true},{"reason_code":"core.invalid_argument","code":"INVALID_ARGUMENT","message_key":"errors.invalid_argument","user_message":"操作参数无效","retryable":false}]}`

func newShowcaseFaultMappers() (*fault.Mapper, *fault.Mapper, error) {
	defaultMapper, err := fault.NewDefaultMapper()
	if err != nil {
		return nil, nil, err
	}
	previewMapper := fault.NewMapper()
	catalog, err := fault.DecodeCatalog([]byte(showcaseFaultCatalogJSON))
	if err != nil {
		return nil, nil, err
	}
	if err := previewMapper.BindReason(errShowcasePreviewSentinel, catalog, "showcase.preview_unavailable"); err != nil {
		return nil, nil, err
	}
	return defaultMapper, previewMapper, nil
}
