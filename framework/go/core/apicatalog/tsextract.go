package apicatalog

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
)

var (
	tsBlockExport = regexp.MustCompile(`export\s+(type\s+)?\{([^}]+)\}`)
	tsFuncExport  = regexp.MustCompile(`(?m)^export\s+(?:async\s+)?function\s+([A-Za-z_][A-Za-z0-9_]*)`)
	tsTypeExport  = regexp.MustCompile(`(?m)^export\s+type\s+([A-Za-z_][A-Za-z0-9_]*)\b`)
)

func ExtractTypeScriptDir(dir string) ([]Symbol, error) {
	dir = strings.TrimSpace(dir)
	if dir == "" {
		return nil, ErrInvalidCatalog
	}
	info, err := os.Stat(dir)
	if err != nil || !info.IsDir() {
		return nil, fmt.Errorf("%w: 无法读取目录 %s", ErrInvalidCatalog, dir)
	}
	found := map[string]Symbol{}
	walkErr := filepath.WalkDir(dir, func(path string, entry os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if entry.IsDir() {
			name := entry.Name()
			if name == "node_modules" || name == "dist" || name == ".git" {
				return filepath.SkipDir
			}
			return nil
		}
		name := entry.Name()
		if strings.HasPrefix(name, ".") || strings.Contains(name, ".test.") || strings.HasSuffix(name, ".d.ts") {
			return nil
		}
		if !strings.HasSuffix(name, ".ts") && !strings.HasSuffix(name, ".tsx") {
			return nil
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return nil
		}
		packagePath := packagePathFromDir(filepath.Dir(path))
		if strings.Contains(filepath.ToSlash(path), "/framework/ui-kit/") {
			packagePath = "framework/ui-kit/src"
		}
		for _, symbol := range extractTypeScript(packagePath, string(data)) {
			found[symbol.Name] = symbol
		}
		return nil
	})
	if walkErr != nil {
		return nil, walkErr
	}
	symbols := make([]Symbol, 0, len(found))
	for _, symbol := range found {
		symbols = append(symbols, symbol)
	}
	sort.Slice(symbols, func(i, j int) bool { return symbols[i].Name < symbols[j].Name })
	return symbols, nil
}

func extractTypeScript(packagePath, source string) []Symbol {
	symbols := make([]Symbol, 0)
	for _, match := range tsBlockExport.FindAllStringSubmatch(source, -1) {
		kind := KindFunction
		if strings.TrimSpace(match[1]) != "" {
			kind = KindType
		}
		for _, name := range strings.Split(match[2], ",") {
			name = strings.TrimSpace(name)
			name = strings.TrimPrefix(name, "type ")
			name = strings.TrimSpace(name)
			if name == "" {
				continue
			}
			symbols = append(symbols, tsSymbol(packagePath, name, kind))
		}
	}
	for _, match := range tsFuncExport.FindAllStringSubmatch(source, -1) {
		symbols = append(symbols, tsSymbol(packagePath, match[1], KindFunction))
	}
	for _, match := range tsTypeExport.FindAllStringSubmatch(source, -1) {
		if strings.Contains(match[0], "{") {
			continue
		}
		symbols = append(symbols, tsSymbol(packagePath, match[1], KindType))
	}
	return symbols
}

func tsSymbol(packagePath, name string, kind Kind) Symbol {
	signature := "export function " + name
	if kind == KindType {
		signature = "export type " + name
	}
	sum := sha256.Sum256([]byte(signature))
	return Symbol{Package: packagePath, Name: name, Kind: kind, Signature: signature, SignatureSHA256: hex.EncodeToString(sum[:])}
}
