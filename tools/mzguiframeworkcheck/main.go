package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"

	"github.com/heiyeluren/mz-gui/framework/go/core/requirements"
)

func main() {
	os.Exit(run(os.Args[1:], os.Stdout, os.Stderr))
}

func run(args []string, stdout io.Writer, stderr io.Writer) int {
	flags := flag.NewFlagSet("mzguiframeworkcheck", flag.ContinueOnError)
	flags.SetOutput(stderr)
	root := flags.String("root", ".", "仓库根目录")
	matrix := flags.String("matrix", "framework/requirements/v1.0.json", "需求矩阵路径")
	jsonOutput := flags.Bool("json", false, "输出 JSON 汇总")
	requireComplete := flags.Bool("require-complete", false, "要求所有需求均已实现且三台兼容设备无未验证项")
	requireMacosArm64Complete := flags.Bool("require-macos-arm64-complete", false, "要求所有需求均已实现且 macos-arm64 无未验证/交叉编译项")
	requireMacosX8664Complete := flags.Bool("require-macos-x86_64-complete", false, "要求所有需求均已实现且 macos-x86_64 无未验证/交叉编译项")
	if err := flags.Parse(args); err != nil {
		return 1
	}
	absoluteRoot, err := filepath.Abs(*root)
	if err != nil {
		fmt.Fprintf(stderr, "解析仓库根目录失败: %v\n", err)
		return 1
	}
	matrixPath := *matrix
	if !filepath.IsAbs(matrixPath) {
		matrixPath = filepath.Join(absoluteRoot, matrixPath)
	}
	data, err := os.ReadFile(matrixPath)
	if err != nil {
		fmt.Fprintf(stderr, "读取 Framework 需求矩阵失败: %v\n", err)
		return 1
	}
	catalog, err := requirements.Decode(data)
	if err != nil {
		fmt.Fprintln(stderr, err)
		return 1
	}
	if err := catalog.ValidateRepository(absoluteRoot); err != nil {
		fmt.Fprintln(stderr, err)
		return 1
	}
	summary := catalog.Summary()
	if *jsonOutput {
		encoder := json.NewEncoder(stdout)
		encoder.SetIndent("", "  ")
		if err := encoder.Encode(summary); err != nil {
			fmt.Fprintf(stderr, "输出 Framework 汇总失败: %v\n", err)
			return 1
		}
	} else {
		fmt.Fprintf(stdout, "Framework %s: total=%d implemented=%d partial=%d missing=%d deferred=%d\n",
			summary.Release, summary.Total, summary.Implemented, summary.Partial, summary.Missing, summary.Deferred)
	}
	if *requireComplete && !complete(summary) {
		fmt.Fprintln(stderr, "Framework v1.0 尚未达到完整发布门禁")
		return 2
	}
	if *requireMacosArm64Complete && !macosArm64Complete(summary) {
		fmt.Fprintln(stderr, "Framework v1.0 尚未达到 macos-arm64 完成门禁")
		return 2
	}
	if *requireMacosX8664Complete && !macosX8664Complete(summary) {
		fmt.Fprintln(stderr, "Framework v1.0 尚未达到 macos-x86_64 完成门禁")
		return 2
	}
	return 0
}

func implementedComplete(summary requirements.Summary) bool {
	return summary.Implemented == summary.Total && summary.Partial == 0 && summary.Missing == 0 && summary.Deferred == 0
}

func complete(summary requirements.Summary) bool {
	if !implementedComplete(summary) {
		return false
	}
	for _, platform := range summary.Platforms {
		if platform.NotVerified != 0 || platform.CrossCompiled != 0 {
			return false
		}
	}
	return true
}

func macosArm64Complete(summary requirements.Summary) bool {
	if !implementedComplete(summary) {
		return false
	}
	platform, ok := summary.Platforms["macos-arm64"]
	if !ok {
		return false
	}
	return platform.NotVerified == 0 && platform.CrossCompiled == 0
}

func macosX8664Complete(summary requirements.Summary) bool {
	if !implementedComplete(summary) {
		return false
	}
	platform, ok := summary.Platforms["macos-x86_64"]
	if !ok {
		return false
	}
	return platform.NotVerified == 0 && platform.CrossCompiled == 0
}
