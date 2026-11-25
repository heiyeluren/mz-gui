package apicatalog

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

func ExtractServiceMethods(dir string) ([]Symbol, error) {
	dir = strings.TrimSpace(dir)
	if dir == "" {
		return nil, ErrInvalidCatalog
	}
	info, err := os.Stat(dir)
	if err != nil || !info.IsDir() {
		return nil, fmt.Errorf("%w: 无法读取目录 %s", ErrInvalidCatalog, dir)
	}
	set := token.NewFileSet()
	found := map[string]Symbol{}
	walkErr := filepath.WalkDir(dir, func(path string, entry os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if entry.IsDir() {
			name := entry.Name()
			if name == "vendor" || name == "testdata" || name == ".git" {
				return filepath.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(entry.Name(), ".go") || strings.HasSuffix(entry.Name(), "_test.go") {
			return nil
		}
		file, err := parser.ParseFile(set, path, nil, 0)
		if err != nil {
			return nil
		}
		packagePath := packagePathFromDir(filepath.Dir(path))
		for _, decl := range file.Decls {
			fn, ok := decl.(*ast.FuncDecl)
			if !ok || fn.Name == nil || !fn.Name.IsExported() || fn.Recv == nil || len(fn.Recv.List) == 0 {
				continue
			}
			receiver := receiverName(fn.Recv.List[0].Type)
			if !strings.HasSuffix(receiver, "Service") {
				continue
			}
			name := receiver + "." + fn.Name.Name
			signature := printNode(set, fn.Type)
			sum := sha256.Sum256([]byte(signature))
			found[name] = Symbol{
				Package: packagePath, Name: name, Kind: KindMethod,
				Signature: signature, SignatureSHA256: hex.EncodeToString(sum[:]),
			}
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
