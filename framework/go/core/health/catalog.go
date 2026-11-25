package health

import (
	"bytes"
	_ "embed"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"sort"

	"github.com/heiyeluren/mz-gui/framework/go/core/model"
)

//go:embed v1.json
var builtinCatalogJSON []byte

type Catalog struct {
	SchemaRef     string       `json:"$schema"`
	SchemaVersion int          `json:"schema_version"`
	Release       string       `json:"release"`
	Checks        []Definition `json:"checks"`

	byID map[string]Definition
}

func BuiltinCatalog() (Catalog, error) {
	return Decode(builtinCatalogJSON)
}

func Decode(data []byte) (Catalog, error) {
	var catalog Catalog
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&catalog); err != nil {
		return Catalog{}, fmt.Errorf("%w: %v", ErrInvalidCatalog, err)
	}
	if err := rejectTrailingJSON(decoder); err != nil {
		return Catalog{}, err
	}
	if err := catalog.validate(); err != nil {
		return Catalog{}, err
	}
	return catalog, nil
}

func (catalog Catalog) Lookup(checkID string) (Definition, bool) {
	definition, exists := catalog.byID[checkID]
	definition.ReasonCodes = append([]string(nil), definition.ReasonCodes...)
	return definition, exists
}

func (catalog Catalog) Definitions() []Definition {
	definitions := append([]Definition(nil), catalog.Checks...)
	for index := range definitions {
		definitions[index].ReasonCodes = append([]string(nil), definitions[index].ReasonCodes...)
	}
	sort.Slice(definitions, func(left int, right int) bool {
		return definitions[left].CheckID < definitions[right].CheckID
	})
	return definitions
}

func (catalog *Catalog) validate() error {
	version, err := model.ParseVersion(catalog.Release)
	if catalog.SchemaVersion != SchemaVersion || err != nil || version.Major == 0 || version.Prerelease != "" || version.Build != "" || len(catalog.Checks) == 0 {
		return ErrInvalidCatalog
	}
	catalog.byID = make(map[string]Definition, len(catalog.Checks))
	for index, definition := range catalog.Checks {
		if err := definition.Validate(); err != nil {
			return fmt.Errorf("%w: %s", ErrInvalidCatalog, definition.CheckID)
		}
		if _, exists := catalog.byID[definition.CheckID]; exists {
			return fmt.Errorf("%w: 重复 check_id %s", ErrInvalidCatalog, definition.CheckID)
		}
		definition.ReasonCodes = append([]string(nil), definition.ReasonCodes...)
		catalog.Checks[index] = definition
		lookupDefinition := definition
		lookupDefinition.ReasonCodes = append([]string(nil), definition.ReasonCodes...)
		catalog.byID[definition.CheckID] = lookupDefinition
	}
	return nil
}

func rejectTrailingJSON(decoder *json.Decoder) error {
	var trailing any
	if err := decoder.Decode(&trailing); !errors.Is(err, io.EOF) {
		return fmt.Errorf("%w: JSON 包含额外内容", ErrInvalidCatalog)
	}
	return nil
}
