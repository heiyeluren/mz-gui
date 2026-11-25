package panicguard

import (
	"bytes"
	"encoding/json"
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"math"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"strings"
)

type ProduceProof struct {
	Methods []string `json:"methods"`
	Covered []string `json:"covered"`
	Missing []string `json:"missing"`
	Percent float64  `json:"covered_percent"`
}

type ProduceGate struct {
	MinCoveredPercent float64 `json:"min_covered_percent"`
	Strict            bool    `json:"strict"`
}

type ProduceInventory struct {
	CallerID string       `json:"caller_id"`
	Gate     ProduceGate  `json:"gate"`
	Proof    ProduceProof `json:"proof"`
}

func DecodeProduceInventory(data []byte) (ProduceInventory, error) {
	var raw struct {
		CallerID string       `json:"caller_id"`
		Gate     *ProduceGate `json:"gate"`
		Proof    ProduceProof `json:"proof"`
	}
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&raw); err != nil || decoder.More() || strings.TrimSpace(raw.CallerID) == "" ||
		raw.Gate == nil || raw.Gate.MinCoveredPercent < 0 || raw.Gate.MinCoveredPercent > 100 {
		return ProduceInventory{}, fmt.Errorf("Produce 覆盖 inventory 无效")
	}
	return ProduceInventory{CallerID: raw.CallerID, Gate: *raw.Gate, Proof: raw.Proof}, nil
}

func (inventory ProduceInventory) Drift(proof ProduceProof) error {
	if !sameStrings(inventory.Proof.Missing, proof.Missing) || !sameStrings(inventory.Proof.Covered, proof.Covered) {
		return fmt.Errorf("Produce 覆盖 inventory 与源码漂移: inventory_missing=%d actual_missing=%d", len(inventory.Proof.Missing), len(proof.Missing))
	}
	return nil
}

func (inventory ProduceInventory) EnforceGate(proof ProduceProof) error {
	if proof.Percent+0.0001 < inventory.Gate.MinCoveredPercent {
		return fmt.Errorf("Produce 覆盖率 %.2f 低于门禁 %.2f", proof.Percent, inventory.Gate.MinCoveredPercent)
	}
	if inventory.Gate.Strict && len(proof.Missing) > 0 {
		return fmt.Errorf("Produce 严格门禁失败: missing=%d", len(proof.Missing))
	}
	return nil
}

func (inventory ProduceInventory) Enforce(proof ProduceProof) error {
	if err := inventory.Drift(proof); err != nil {
		return err
	}
	return inventory.EnforceGate(proof)
}

func sameStrings(left, right []string) bool {
	if len(left) == 0 && len(right) == 0 {
		return true
	}
	return reflect.DeepEqual(left, right)
}

func ProveServiceProduceCoverage(dir string) (ProduceProof, error) {
	dir = strings.TrimSpace(dir)
	if dir == "" {
		return ProduceProof{}, fmt.Errorf("Produce 覆盖扫描目录无效")
	}
	info, err := os.Stat(dir)
	if err != nil || !info.IsDir() {
		return ProduceProof{}, fmt.Errorf("无法读取 Produce 覆盖扫描目录 %s", dir)
	}
	set := token.NewFileSet()
	covered := map[string]struct{}{}
	missing := map[string]struct{}{}
	methods := map[string]struct{}{}
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
		for _, decl := range file.Decls {
			fn, ok := decl.(*ast.FuncDecl)
			if !ok || fn.Name == nil || !fn.Name.IsExported() || fn.Recv == nil || len(fn.Recv.List) == 0 {
				continue
			}
			receiver := receiverName(fn.Recv.List[0].Type)
			if !strings.HasSuffix(receiver, "Service") || !returnsError(fn) {
				continue
			}
			name := receiver + "." + fn.Name.Name
			methods[name] = struct{}{}
			if functionCallsProduce(fn) {
				covered[name] = struct{}{}
				continue
			}
			missing[name] = struct{}{}
		}
		return nil
	})
	if walkErr != nil {
		return ProduceProof{}, walkErr
	}
	proof := ProduceProof{
		Methods: sortedKeys(methods),
		Covered: sortedKeys(covered),
		Missing: sortedKeys(missing),
	}
	if len(proof.Methods) > 0 {
		proof.Percent = math.Round((float64(len(proof.Covered))/float64(len(proof.Methods)))*10000) / 100
	}
	return proof, nil
}

func functionCallsProduce(fn *ast.FuncDecl) bool {
	if fn.Body == nil {
		return false
	}
	found := false
	ast.Inspect(fn.Body, func(node ast.Node) bool {
		sel, ok := node.(*ast.SelectorExpr)
		if !ok || sel.Sel == nil {
			return true
		}
		switch sel.Sel.Name {
		case "Produce", "ProduceResult", "GuardedProduce", "GuardedCall", "Guard", "GuardResult", "GuardRequest", "GuardRequestResult":
			found = true
			return false
		}
		return true
	})
	return found
}

func returnsError(fn *ast.FuncDecl) bool {
	if fn.Type == nil || fn.Type.Results == nil {
		return false
	}
	for _, field := range fn.Type.Results.List {
		if ident, ok := field.Type.(*ast.Ident); ok && ident.Name == "error" {
			return true
		}
	}
	return false
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

func sortedKeys(values map[string]struct{}) []string {
	out := make([]string, 0, len(values))
	for value := range values {
		out = append(out, value)
	}
	sort.Strings(out)
	return out
}
