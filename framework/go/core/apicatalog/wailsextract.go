package apicatalog

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"unicode"
	"unicode/utf8"
)

const wailsApplicationImport = "github.com/wailsapp/wails/v3/pkg/application"

type WailsBinding struct {
	Name     string `json:"name"`
	TypeName string `json:"type_name"`
	File     string `json:"file"`
}

type WailsBindingProof struct {
	Bindings       []WailsBinding `json:"bindings"`
	BoundMethods   []string       `json:"bound_methods"`
	UnboundMethods []string       `json:"unbound_methods"`
	MissingTypes   []string       `json:"missing_types"`
	FrontendAPIs   []string       `json:"frontend_apis,omitempty"`
}

func ExtractWailsBindings(dir string) ([]WailsBinding, error) {
	dir = strings.TrimSpace(dir)
	if dir == "" {
		return nil, ErrInvalidCatalog
	}
	info, err := os.Stat(dir)
	if err != nil || !info.IsDir() {
		return nil, fmt.Errorf("%w: 无法读取 Wails 绑定目录 %s", ErrInvalidCatalog, dir)
	}
	found := map[string]WailsBinding{}
	walkErr := filepath.WalkDir(dir, func(path string, entry os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if entry.IsDir() {
			name := entry.Name()
			if name == "vendor" || name == "testdata" || name == ".git" || name == "node_modules" {
				return filepath.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(entry.Name(), ".go") || strings.HasSuffix(entry.Name(), "_test.go") {
			return nil
		}
		for _, binding := range extractWailsBindingsFile(path) {
			key := binding.Name + "\x00" + binding.TypeName + "\x00" + binding.File
			found[key] = binding
		}
		return nil
	})
	if walkErr != nil {
		return nil, walkErr
	}
	bindings := make([]WailsBinding, 0, len(found))
	for _, binding := range found {
		bindings = append(bindings, binding)
	}
	sort.Slice(bindings, func(i, j int) bool {
		if bindings[i].Name == bindings[j].Name {
			if bindings[i].TypeName == bindings[j].TypeName {
				return bindings[i].File < bindings[j].File
			}
			return bindings[i].TypeName < bindings[j].TypeName
		}
		return bindings[i].Name < bindings[j].Name
	})
	return bindings, nil
}

func ProveWailsBindingCoverage(bindRoot, serviceRoot string) (WailsBindingProof, error) {
	bindings, err := ExtractWailsBindings(bindRoot)
	if err != nil {
		return WailsBindingProof{}, err
	}
	methods, err := ExtractServiceMethods(serviceRoot)
	if err != nil {
		return WailsBindingProof{}, err
	}
	typeNames := make(map[string]struct{})
	for _, method := range methods {
		if index := strings.Index(method.Name, "."); index > 0 {
			typeNames[method.Name[:index]] = struct{}{}
		}
	}
	resolved := make(map[string]struct{})
	missing := make([]string, 0)
	for _, binding := range bindings {
		typeName := binding.TypeName
		if typeName != "" {
			if _, appService := typeNames[typeName]; !appService {
				if inferred := inferServiceType(binding.Name, typeNames); inferred != "" {
					typeName = inferred
				}
			}
		}
		if typeName == "" {
			typeName = inferServiceType(binding.Name, typeNames)
		}
		if typeName == "" {
			if _, exists := typeNames[binding.Name]; exists {
				typeName = binding.Name
			}
		}
		if typeName == "" {
			missing = append(missing, binding.Name)
			continue
		}
		resolved[typeName] = struct{}{}
	}
	sort.Strings(missing)
	bound := make([]string, 0)
	unbound := make([]string, 0)
	for _, method := range methods {
		typeName := method.Name
		if index := strings.Index(method.Name, "."); index > 0 {
			typeName = method.Name[:index]
		}
		if _, ok := resolved[typeName]; ok {
			bound = append(bound, method.Name)
			continue
		}
		unbound = append(unbound, method.Name)
	}
	sort.Strings(bound)
	sort.Strings(unbound)
	return WailsBindingProof{
		Bindings: bindings, BoundMethods: bound, UnboundMethods: unbound, MissingTypes: missing,
	}, nil
}

func extractWailsBindingsFile(path string) []WailsBinding {
	set := token.NewFileSet()
	file, err := parser.ParseFile(set, path, nil, 0)
	if err != nil {
		return nil
	}
	aliases := make(map[string]struct{})
	for _, spec := range file.Imports {
		importPath, err := strconv.Unquote(spec.Path.Value)
		if err != nil || importPath != wailsApplicationImport {
			continue
		}
		name := "application"
		if spec.Name != nil && spec.Name.Name != "" && spec.Name.Name != "." && spec.Name.Name != "_" {
			name = spec.Name.Name
		}
		aliases[name] = struct{}{}
	}
	if len(aliases) == 0 {
		return nil
	}
	variableTypes := inferBindingVariableTypes(file)
	bindings := make([]WailsBinding, 0)
	fileName := filepath.Base(path)
	ast.Inspect(file, func(node ast.Node) bool {
		call, ok := node.(*ast.CallExpr)
		if !ok || len(call.Args) == 0 {
			return true
		}
		sel, ok := call.Fun.(*ast.SelectorExpr)
		if !ok || sel.Sel == nil || sel.Sel.Name != "NewService" {
			return true
		}
		ident, ok := sel.X.(*ast.Ident)
		if !ok {
			return true
		}
		if _, known := aliases[ident.Name]; !known {
			return true
		}
		name, typeName, ok := bindingFromExpr(call.Args[0])
		if !ok {
			return true
		}
		if typeName == "" {
			typeName = variableTypes[name]
		}
		bindings = append(bindings, WailsBinding{Name: name, TypeName: typeName, File: fileName})
		return true
	})
	return bindings
}

// inferBindingVariableTypes 只依据语法恢复局部绑定类型，避免依赖应用服务命名约定。
func inferBindingVariableTypes(file *ast.File) map[string]string {
	types := make(map[string]string)
	ast.Inspect(file, func(node ast.Node) bool {
		switch value := node.(type) {
		case *ast.FuncDecl:
			if value.Type == nil || value.Type.Params == nil {
				return true
			}
			for _, field := range value.Type.Params.List {
				typeName := typeNameFromExpr(field.Type)
				for _, name := range field.Names {
					if typeName != "" {
						types[name.Name] = typeName
					}
				}
			}
		case *ast.AssignStmt:
			for index, left := range value.Lhs {
				ident, ok := left.(*ast.Ident)
				if !ok || index >= len(value.Rhs) {
					continue
				}
				if typeName := bindingTypeFromInitializer(value.Rhs[index]); typeName != "" {
					types[ident.Name] = typeName
				}
			}
		case *ast.ValueSpec:
			explicitType := typeNameFromExpr(value.Type)
			for index, name := range value.Names {
				typeName := explicitType
				if typeName == "" && index < len(value.Values) {
					typeName = bindingTypeFromInitializer(value.Values[index])
				}
				if typeName != "" {
					types[name.Name] = typeName
				}
			}
		}
		return true
	})
	return types
}

func bindingTypeFromInitializer(expr ast.Expr) string {
	switch value := expr.(type) {
	case *ast.UnaryExpr:
		return bindingTypeFromInitializer(value.X)
	case *ast.CompositeLit:
		return typeNameFromExpr(value.Type)
	case *ast.CallExpr:
		switch function := value.Fun.(type) {
		case *ast.SelectorExpr:
			if function.Sel == nil {
				return ""
			}
			if function.Sel.Name == "New" {
				if packageName, ok := function.X.(*ast.Ident); ok {
					return packageName.Name + ".New"
				}
			}
		}
	}
	return ""
}

func bindingFromExpr(expr ast.Expr) (string, string, bool) {
	switch value := expr.(type) {
	case *ast.Ident:
		return value.Name, "", true
	case *ast.UnaryExpr:
		if value.Op == token.AND {
			return bindingFromExpr(value.X)
		}
	case *ast.CompositeLit:
		typeName := typeNameFromExpr(value.Type)
		if typeName == "" {
			return "", "", false
		}
		return typeName, typeName, true
	case *ast.CallExpr:
		funcName := ""
		switch fun := value.Fun.(type) {
		case *ast.Ident:
			funcName = fun.Name
		case *ast.SelectorExpr:
			if fun.Sel != nil {
				funcName = fun.Sel.Name
			}
		}
		if funcName == "" {
			return "", "", false
		}
		typeName := strings.TrimPrefix(funcName, "New")
		if typeName == funcName {
			typeName = ""
		}
		return funcName, typeName, true
	}
	return "", "", false
}

func typeNameFromExpr(expr ast.Expr) string {
	switch value := expr.(type) {
	case *ast.Ident:
		return value.Name
	case *ast.SelectorExpr:
		if value.Sel != nil {
			return value.Sel.Name
		}
	case *ast.StarExpr:
		return typeNameFromExpr(value.X)
	}
	return ""
}

func inferServiceType(ident string, typeNames map[string]struct{}) string {
	if ident == "" {
		return ""
	}
	if _, ok := typeNames[ident]; ok {
		return ident
	}
	exported := exportIdent(ident)
	if _, ok := typeNames[exported]; ok {
		return exported
	}
	for typeName := range typeNames {
		if lowerFirst(typeName) == ident {
			return typeName
		}
	}
	return ""
}

func exportIdent(value string) string {
	r, size := utf8.DecodeRuneInString(value)
	if r == utf8.RuneError {
		return value
	}
	return string(unicode.ToUpper(r)) + value[size:]
}

func lowerFirst(value string) string {
	r, size := utf8.DecodeRuneInString(value)
	if r == utf8.RuneError {
		return value
	}
	return string(unicode.ToLower(r)) + value[size:]
}
