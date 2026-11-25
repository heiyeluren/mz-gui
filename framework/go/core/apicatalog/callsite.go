package apicatalog

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"math"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
)

const modulePath = "github.com/heiyeluren/mz-gui"

type CallsiteProof struct {
	CallerID        string   `json:"caller_id"`
	Roots           []string `json:"roots"`
	Declared        []string `json:"declared"`
	Found           []string `json:"found"`
	Missing         []string `json:"missing"`
	Extra           []string `json:"extra"`
	CoveredPercent  float64  `json:"covered_percent"`
	ScannedGoFiles  int      `json:"scanned_go_files"`
}

func ProveCallsiteCoverage(catalog Catalog, usage CallerUsage, roots []string) (CallsiteProof, error) {
	if !callerIDPattern.MatchString(usage.CallerID) || len(usage.APIIDs) == 0 || hasDuplicates(usage.APIIDs) {
		return CallsiteProof{}, ErrInvalidGovernance
	}
	for _, apiID := range usage.APIIDs {
		if !apiIDPattern.MatchString(apiID) {
			return CallsiteProof{}, ErrInvalidGovernance
		}
	}
	cleaned := make([]string, 0, len(roots))
	for _, root := range roots {
		root = strings.TrimSpace(root)
		if root == "" {
			return CallsiteProof{}, fmt.Errorf("%w: 调用点扫描根目录无效", ErrInvalidCatalog)
		}
		cleaned = append(cleaned, root)
	}
	if len(cleaned) == 0 {
		return CallsiteProof{}, fmt.Errorf("%w: 缺少调用点扫描根目录", ErrInvalidCatalog)
	}

	used, scanned, err := scanCallSites(cleaned)
	if err != nil {
		return CallsiteProof{}, err
	}

	found := make([]string, 0)
	missing := make([]string, 0)
	for _, apiID := range usage.APIIDs {
		api, ok := catalog.Lookup(apiID)
		if !ok || !used.matches(api) {
			missing = append(missing, apiID)
			continue
		}
		found = append(found, apiID)
	}

	declaredSet := make(map[string]struct{}, len(usage.APIIDs))
	for _, apiID := range usage.APIIDs {
		declaredSet[apiID] = struct{}{}
	}
	extra := make([]string, 0)
	for _, api := range catalog.APIs {
		if _, declared := declaredSet[api.APIID]; declared {
			continue
		}
		if used.matches(api) {
			extra = append(extra, api.APIID)
		}
	}
	sort.Strings(extra)

	percent := 0.0
	if len(usage.APIIDs) > 0 {
		percent = math.Round((float64(len(found))/float64(len(usage.APIIDs)))*10000) / 100
	}
	return CallsiteProof{
		CallerID: usage.CallerID, Roots: append([]string(nil), cleaned...),
		Declared: append([]string(nil), usage.APIIDs...), Found: found, Missing: missing, Extra: extra,
		CoveredPercent: percent, ScannedGoFiles: scanned,
	}, nil
}

type usedSymbols struct {
	functions map[string]map[string]struct{}
	methods   map[string]map[string]struct{}
	packages  map[string]struct{}
}

func (used usedSymbols) matches(api API) bool {
	importPath := catalogImportPath(api.Package)
	switch api.Kind {
	case KindFunction, KindCLI, KindType:
		_, ok := used.functions[importPath][api.Symbol]
		return ok
	case KindMethod:
		method := api.Symbol
		if index := strings.LastIndex(method, "."); index >= 0 {
			method = method[index+1:]
		}
		_, ok := used.methods[importPath][method]
		return ok
	default:
		_, ok := used.packages[importPath]
		return ok
	}
}

func scanCallSites(roots []string) (usedSymbols, int, error) {
	used := usedSymbols{
		functions: make(map[string]map[string]struct{}),
		methods:   make(map[string]map[string]struct{}),
		packages:  make(map[string]struct{}),
	}
	scanned := 0
	for _, root := range roots {
		info, err := os.Stat(root)
		if err != nil || !info.IsDir() {
			return usedSymbols{}, 0, fmt.Errorf("%w: 无法读取调用点目录 %s", ErrInvalidCatalog, root)
		}
		walkErr := filepath.WalkDir(root, func(path string, entry os.DirEntry, walkErr error) error {
			if walkErr != nil {
				return walkErr
			}
			if entry.IsDir() {
				name := entry.Name()
				if name == "vendor" || name == "node_modules" || name == ".git" || name == "testdata" {
					return filepath.SkipDir
				}
				return nil
			}
			if !strings.HasSuffix(entry.Name(), ".go") || strings.HasSuffix(entry.Name(), "_test.go") || strings.HasPrefix(entry.Name(), ".") {
				return nil
			}
			if err := inspectGoFile(path, &used); err != nil {
				return nil
			}
			scanned++
			return nil
		})
		if walkErr != nil {
			return usedSymbols{}, 0, walkErr
		}
	}
	return used, scanned, nil
}

func inspectGoFile(path string, used *usedSymbols) error {
	set := token.NewFileSet()
	file, err := parser.ParseFile(set, path, nil, 0)
	if err != nil {
		return err
	}
	aliases := make(map[string]string)
	imported := make([]string, 0)
	for _, spec := range file.Imports {
		importPath, err := strconv.Unquote(spec.Path.Value)
		if err != nil {
			continue
		}
		name := ""
		if spec.Name != nil {
			name = spec.Name.Name
		}
		if name == "_" {
			continue
		}
		if name == "" {
			parts := strings.Split(importPath, "/")
			name = parts[len(parts)-1]
		}
		if name != "." {
			aliases[name] = importPath
		}
		imported = append(imported, importPath)
		used.packages[importPath] = struct{}{}
	}
	ast.Inspect(file, func(node ast.Node) bool {
		selector, ok := node.(*ast.SelectorExpr)
		if !ok || selector.Sel == nil {
			return true
		}
		if ident, ok := selector.X.(*ast.Ident); ok {
			if importPath, exists := aliases[ident.Name]; exists {
				addSymbol(used.functions, importPath, selector.Sel.Name)
			}
		}
		for _, importPath := range imported {
			addSymbol(used.methods, importPath, selector.Sel.Name)
		}
		return true
	})
	return nil
}

func addSymbol(target map[string]map[string]struct{}, importPath, name string) {
	if target[importPath] == nil {
		target[importPath] = make(map[string]struct{})
	}
	target[importPath][name] = struct{}{}
}

func catalogImportPath(pkg string) string {
	pkg = strings.TrimSpace(pkg)
	if strings.HasPrefix(pkg, "github.com/") {
		return pkg
	}
	return modulePath + "/" + strings.TrimPrefix(pkg, "/")
}
