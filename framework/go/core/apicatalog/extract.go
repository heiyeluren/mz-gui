package apicatalog

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"go/ast"
	"go/parser"
	"go/printer"
	"go/token"
	"os"
	"path/filepath"
	"strings"
)

type Symbol struct {
	Package         string
	Name            string
	Kind            Kind
	Signature       string
	SignatureSHA256 string
}

func ExtractDir(dir string) ([]Symbol, error) {
	dir = strings.TrimSpace(dir)
	if dir == "" {
		return nil, ErrInvalidCatalog
	}
	info, err := os.Stat(dir)
	if err != nil || !info.IsDir() {
		return nil, fmt.Errorf("%w: 无法读取目录 %s", ErrInvalidCatalog, dir)
	}
	set := token.NewFileSet()
	packages, err := parser.ParseDir(set, dir, func(file os.FileInfo) bool {
		name := file.Name()
		return strings.HasSuffix(name, ".go") && !strings.HasSuffix(name, "_test.go") && !strings.HasPrefix(name, ".")
	}, 0)
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrInvalidCatalog, err)
	}
	symbols := make([]Symbol, 0)
	for _, pkg := range packages {
		if strings.HasSuffix(pkg.Name, "_test") {
			continue
		}
		for fileName, file := range pkg.Files {
			if strings.Contains(filepath.Base(fileName), "_test.go") {
				continue
			}
			packagePath := packagePathFromDir(dir)
			symbols = append(symbols, extractFile(set, packagePath, pkg.Name, file)...)
		}
	}
	return symbols, nil
}

func extractFile(set *token.FileSet, packagePath, packageName string, file *ast.File) []Symbol {
	symbols := make([]Symbol, 0)
	for _, decl := range file.Decls {
		fn, ok := decl.(*ast.FuncDecl)
		if !ok || fn.Name == nil || !fn.Name.IsExported() {
			continue
		}
		signature := printNode(set, fn.Type)
		name := fn.Name.Name
		kind := KindFunction
		if fn.Recv != nil && len(fn.Recv.List) > 0 {
			kind = KindMethod
			name = receiverName(fn.Recv.List[0].Type) + "." + fn.Name.Name
		}
		sum := sha256.Sum256([]byte(signature))
		symbols = append(symbols, Symbol{
			Package:         packagePath,
			Name:            name,
			Kind:            kind,
			Signature:       signature,
			SignatureSHA256: hex.EncodeToString(sum[:]),
		})
		_ = packageName
	}
	return symbols
}

func receiverName(expr ast.Expr) string {
	switch value := expr.(type) {
	case *ast.Ident:
		return value.Name
	case *ast.StarExpr:
		return receiverName(value.X)
	case *ast.IndexExpr:
		return receiverName(value.X)
	default:
		return "Receiver"
	}
}

func printNode(set *token.FileSet, node ast.Node) string {
	var builder strings.Builder
	if err := printer.Fprint(&builder, set, node); err != nil {
		return ""
	}
	return strings.Join(strings.Fields(builder.String()), " ")
}

func packagePathFromDir(dir string) string {
	dir = filepath.ToSlash(dir)
	marker := "/framework/go/"
	if index := strings.Index(dir, marker); index >= 0 {
		return "framework/go/" + strings.TrimPrefix(dir[index+len(marker):], "/")
	}
	return filepath.Base(dir)
}
