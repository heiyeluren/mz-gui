package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"

	"github.com/heiyeluren/mz-gui/framework/go/core/capability"
)

func main() {
	os.Exit(run(os.Args[1:], os.Stdout, os.Stderr))
}

func run(args []string, stdout io.Writer, stderr io.Writer) int {
	flags := flag.NewFlagSet("mzguicapabilityproposal", flag.ContinueOnError)
	flags.SetOutput(stderr)
	root := flags.String("root", ".", "仓库根目录")
	catalogPath := flags.String("catalog", "framework/capabilities/catalog.json", "Capability Catalog 路径")
	proposalPath := flags.String("proposal", "", "Capability proposal JSON 路径")
	if err := flags.Parse(args); err != nil {
		return 1
	}
	if *proposalPath == "" {
		fmt.Fprintln(stderr, "缺少 --proposal")
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
	proposalData, err := os.ReadFile(resolvePath(absoluteRoot, *proposalPath))
	if err != nil {
		fmt.Fprintf(stderr, "读取 proposal 失败: %v\n", err)
		return 1
	}
	proposal, err := capability.DecodeProposal(proposalData)
	if err != nil {
		fmt.Fprintf(stderr, "解析 proposal 失败: %v\n", err)
		return 1
	}
	review, err := capability.ReviewProposal(catalog, proposal)
	if err != nil {
		fmt.Fprintf(stderr, "proposal 未通过: %v\n", err)
		return 2
	}
	encoder := json.NewEncoder(stdout)
	encoder.SetIndent("", "  ")
	if err := encoder.Encode(review); err != nil {
		fmt.Fprintf(stderr, "输出 review 失败: %v\n", err)
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
