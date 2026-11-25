package main

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"

	"github.com/heiyeluren/mz-gui/framework/go/adapters/artifact"
)

func main() {
	os.Exit(run(os.Args[1:], os.Stdout, os.Stderr))
}

func run(args []string, stdout io.Writer, stderr io.Writer) int {
	flags := flag.NewFlagSet("mzguisbom", flag.ContinueOnError)
	flags.SetOutput(stderr)
	root := flags.String("root", ".", "仓库根目录")
	if err := flags.Parse(args); err != nil {
		return 2
	}
	data, err := os.ReadFile(filepath.Join(*root, "go.mod"))
	if err != nil {
		fmt.Fprintf(stderr, "读取 go.mod 失败: %v\n", err)
		return 1
	}
	components, err := artifact.ParseGoModRequires(data)
	if err != nil {
		fmt.Fprintln(stderr, err)
		return 1
	}
	encoder := json.NewEncoder(stdout)
	encoder.SetIndent("", "  ")
	sum := sha256.Sum256(data)
	if err := encoder.Encode(map[string]any{
		"schema_version": 1,
		"format":         "mzgui-sbom-lite",
		"source": map[string]any{
			"path":   "go.mod",
			"sha256": hex.EncodeToString(sum[:]),
		},
		"components": components,
	}); err != nil {
		fmt.Fprintf(stderr, "输出 SBOM 失败: %v\n", err)
		return 1
	}
	return 0
}
