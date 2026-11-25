package main

import (
	"flag"
	"fmt"
	"io"
	"os"

	"github.com/heiyeluren/mz-gui/framework/go/core/appgenerator"
)

func main() {
	os.Exit(run(os.Args[1:], os.Stderr))
}

func run(args []string, output io.Writer) int {
	flags := flag.NewFlagSet("mzguiappgen", flag.ContinueOnError)
	flags.SetOutput(output)
	manifestPath := flags.String("manifest", "", "应用 mzgui.app.toml 路径")
	catalogPath := flags.String("catalog", "", "Framework capability catalog 路径")
	outputPath := flags.String("output", "", "生成的 manifest_gen.go 路径")
	packageName := flags.String("package", "appmeta", "生成文件的 Go package")
	check := flags.Bool("check", false, "仅检查生成结果是否最新")
	if err := flags.Parse(args); err != nil || flags.NArg() != 0 || *manifestPath == "" || *catalogPath == "" || *outputPath == "" {
		if err == nil {
			fmt.Fprintln(output, "manifest、catalog 和 output 参数不能为空")
		}
		return 2
	}
	options := appgenerator.Options{PackageName: *packageName}
	var err error
	if *check {
		err = appgenerator.CheckFile(*manifestPath, *catalogPath, *outputPath, options)
	} else {
		err = appgenerator.GenerateFile(*manifestPath, *catalogPath, *outputPath, options)
	}
	if err != nil {
		fmt.Fprintf(output, "应用元数据处理失败: %v\n", err)
		return 1
	}
	return 0
}
