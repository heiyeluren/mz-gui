package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"

	"github.com/heiyeluren/mz-gui/framework/go/core/capability"
	"github.com/heiyeluren/mz-gui/framework/go/core/requirements"
)

func main() {
	os.Exit(run(os.Args[1:], os.Stdout, os.Stderr))
}

func run(args []string, stdout io.Writer, stderr io.Writer) int {
	flags := flag.NewFlagSet("mzguicapabilityevidence", flag.ContinueOnError)
	flags.SetOutput(stderr)
	root := flags.String("root", ".", "仓库根目录")
	catalogPath := flags.String("catalog", "framework/capabilities/catalog.json", "Capability Catalog 路径")
	requirementsPath := flags.String("requirements", "framework/requirements/v1.0.json", "Framework requirements 路径")
	if err := flags.Parse(args); err != nil {
		return 1
	}
	absoluteRoot, err := filepath.Abs(*root)
	if err != nil {
		fmt.Fprintf(stderr, "解析仓库根目录失败: %v\n", err)
		return 1
	}
	catalogData, err := os.ReadFile(resolvePath(absoluteRoot, *catalogPath))
	if err != nil {
		fmt.Fprintf(stderr, "读取 Catalog 失败: %v\n", err)
		return 1
	}
	catalog, err := capability.Decode(catalogData)
	if err != nil {
		fmt.Fprintf(stderr, "解析 Catalog 失败: %v\n", err)
		return 1
	}
	requirementsData, err := os.ReadFile(resolvePath(absoluteRoot, *requirementsPath))
	if err != nil {
		fmt.Fprintf(stderr, "读取 requirements 失败: %v\n", err)
		return 1
	}
	requirementCatalog, err := requirements.Decode(requirementsData)
	if err != nil {
		fmt.Fprintf(stderr, "解析 requirements 失败: %v\n", err)
		return 1
	}
	report, err := capability.BuildEvidenceReport(catalog, toEvidenceRequirements(requirementCatalog.Requirements))
	if err != nil {
		fmt.Fprintf(stderr, "生成 evidence report 失败: %v\n", err)
		return 2
	}
	encoder := json.NewEncoder(stdout)
	encoder.SetIndent("", "  ")
	if err := encoder.Encode(report); err != nil {
		fmt.Fprintf(stderr, "输出 evidence report 失败: %v\n", err)
		return 1
	}
	return 0
}

func resolvePath(root, path string) string {
	if filepath.IsAbs(path) {
		return path
	}
	return filepath.Join(root, path)
}

func toEvidenceRequirements(values []requirements.Requirement) []capability.EvidenceRequirement {
	result := make([]capability.EvidenceRequirement, 0, len(values))
	for _, value := range values {
		result = append(result, capability.EvidenceRequirement{
			RequirementID:        value.RequirementID,
			ImplementationStatus: string(value.ImplementationStatus),
			ImplementationRefs:   value.ImplementationRefs,
			ContractRefs:         value.ContractRefs,
			TestRefs:             value.TestRefs,
			EvidenceRefs:         value.EvidenceRefs,
			Gaps:                 value.Gaps,
		})
	}
	return result
}
