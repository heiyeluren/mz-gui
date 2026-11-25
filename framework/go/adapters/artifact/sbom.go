package artifact

import (
	"bufio"
	"bytes"
	"errors"
	"fmt"
	"strings"
)

var ErrInvalidSBOM = errors.New("SBOM 输入无效")

type SBOMComponent struct {
	Name    string `json:"name"`
	Version string `json:"version"`
	Type    string `json:"type"`
}

func ParseGoModRequires(data []byte) ([]SBOMComponent, error) {
	if len(bytes.TrimSpace(data)) == 0 {
		return nil, ErrInvalidSBOM
	}
	scanner := bufio.NewScanner(bytes.NewReader(data))
	inRequire := false
	components := make([]SBOMComponent, 0)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "//") {
			continue
		}
		if strings.HasPrefix(line, "require (") {
			inRequire = true
			continue
		}
		if inRequire && line == ")" {
			inRequire = false
			continue
		}
		if strings.HasPrefix(line, "require ") && !strings.HasPrefix(line, "require (") {
			if component, ok := parseRequireLine(strings.TrimPrefix(line, "require ")); ok {
				components = append(components, component)
			}
			continue
		}
		if inRequire {
			if component, ok := parseRequireLine(line); ok {
				components = append(components, component)
			}
		}
	}
	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("%w: %v", ErrInvalidSBOM, err)
	}
	if len(components) == 0 {
		return nil, ErrInvalidSBOM
	}
	return components, nil
}

func parseRequireLine(line string) (SBOMComponent, bool) {
	if strings.Contains(line, "// indirect") {
		return SBOMComponent{}, false
	}
	line = strings.TrimSpace(strings.Split(line, "//")[0])
	fields := strings.Fields(line)
	if len(fields) < 2 {
		return SBOMComponent{}, false
	}
	return SBOMComponent{Name: fields[0], Version: fields[1], Type: "library"}, true
}
