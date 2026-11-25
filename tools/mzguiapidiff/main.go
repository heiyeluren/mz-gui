package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/heiyeluren/mz-gui/framework/go/core/apicatalog"
	"github.com/heiyeluren/mz-gui/framework/go/core/panicguard"
)

func main() {
	os.Exit(run(os.Args[1:], os.Stdout, os.Stderr))
}

func run(args []string, stdout io.Writer, stderr io.Writer) int {
	flags := flag.NewFlagSet("mzguiapidiff", flag.ContinueOnError)
	flags.SetOutput(stderr)
	oldPath := flags.String("old", "", "旧公共 API catalog")
	newPath := flags.String("new", "", "新公共 API catalog")
	jsonOutput := flags.Bool("json", false, "输出 JSON diff")
	var callerUsagePaths repeatedFlag
	flags.Var(&callerUsagePaths, "caller-usage", "调用方 API 使用清单，可重复传入")
	callsiteCaller := flags.String("callsite-caller", "", "对指定调用方执行生产调用点覆盖证明")
	strictCallsite := flags.Bool("strict-callsite", false, "声明但未命中调用点时返回失败")
	wailsBindRoot := flags.String("wails-bind-root", "", "扫描 application.NewService 绑定的根目录")
	wailsServiceRoot := flags.String("wails-service-root", "", "扫描 *Service 导出方法的根目录")
	produceRoot := flags.String("produce-root", "", "扫描 *Service 是否改走 Produce/Guard")
	produceInventory := flags.String("produce-inventory", "", "对照 Produce 覆盖 inventory")
	var callsiteRoots repeatedFlag
	flags.Var(&callsiteRoots, "callsite-root", "生产调用点扫描根目录，可重复传入")
	if err := flags.Parse(args); err != nil {
		return 1
	}
	if *oldPath == "" || *newPath == "" {
		fmt.Fprintln(stderr, "必须提供 --old 和 --new")
		return 1
	}
	oldCatalog, err := readCatalog(*oldPath)
	if err != nil {
		fmt.Fprintf(stderr, "读取旧 API catalog 失败: %v\n", err)
		return 1
	}
	newCatalog, err := readCatalog(*newPath)
	if err != nil {
		fmt.Fprintf(stderr, "读取新 API catalog 失败: %v\n", err)
		return 1
	}
	diff := apicatalog.Compare(oldCatalog, newCatalog)
	if *jsonOutput {
		encoder := json.NewEncoder(stdout)
		encoder.SetIndent("", "  ")
		if err := encoder.Encode(diff); err != nil {
			fmt.Fprintf(stderr, "输出 API diff 失败: %v\n", err)
			return 1
		}
	} else {
		fmt.Fprintf(stdout, "API diff: breaking=%d warnings=%d compatible=%d\n", len(diff.Breaking), len(diff.Warnings), len(diff.Compatible))
	}
	if diff.HasBreaking() {
		return 2
	}
	if len(callerUsagePaths) > 0 {
		usages, err := readCallerUsages(callerUsagePaths)
		if err != nil {
			fmt.Fprintf(stderr, "读取调用方 API 使用清单失败: %v\n", err)
			return 1
		}
		if err := apicatalog.ValidateGovernance(newCatalog, usages...); err != nil {
			fmt.Fprintf(stderr, "API 治理门禁失败: %v\n", err)
			return 3
		}
		if len(callsiteRoots) > 0 {
			callerID := strings.TrimSpace(*callsiteCaller)
			if callerID == "" {
				fmt.Fprintln(stderr, "提供 --callsite-root 时必须同时提供 --callsite-caller")
				return 1
			}
			var selected *apicatalog.CallerUsage
			for index := range usages {
				if usages[index].CallerID == callerID {
					selected = &usages[index]
					break
				}
			}
			if selected == nil {
				fmt.Fprintf(stderr, "调用方 %s 不在 --caller-usage 清单中\n", callerID)
				return 1
			}
			proof, err := apicatalog.ProveCallsiteCoverage(newCatalog, *selected, callsiteRoots)
			if err != nil {
				fmt.Fprintf(stderr, "生产调用点覆盖证明失败: %v\n", err)
				return 1
			}
			if *jsonOutput {
				encoder := json.NewEncoder(stdout)
				encoder.SetIndent("", "  ")
				if err := encoder.Encode(proof); err != nil {
					fmt.Fprintf(stderr, "输出调用点证明失败: %v\n", err)
					return 1
				}
			} else {
				fmt.Fprintf(stdout, "Callsite %s: covered=%.2f found=%d missing=%d extra=%d files=%d\n",
					proof.CallerID, proof.CoveredPercent, len(proof.Found), len(proof.Missing), len(proof.Extra), proof.ScannedGoFiles)
			}
			if (*strictCallsite || newCatalog.Governance.Coverage.StrictCallsite) && len(proof.Missing) > 0 {
				fmt.Fprintf(stderr, "调用方 %s 缺少生产调用点: %s\n", proof.CallerID, strings.Join(proof.Missing, ","))
				return 4
			}
		}
	}
	bindRoot := strings.TrimSpace(*wailsBindRoot)
	serviceRoot := strings.TrimSpace(*wailsServiceRoot)
	if bindRoot != "" || serviceRoot != "" {
		if bindRoot == "" || serviceRoot == "" {
			fmt.Fprintln(stderr, "提供 Wails 绑定扫描时必须同时提供 --wails-bind-root 和 --wails-service-root")
			return 1
		}
		proof, err := apicatalog.ProveWailsBindingCoverage(bindRoot, serviceRoot)
		if err != nil {
			fmt.Fprintf(stderr, "Wails 绑定覆盖证明失败: %v\n", err)
			return 1
		}
		if len(proof.Bindings) == 0 {
			fmt.Fprintln(stderr, "未扫描到 application.NewService 绑定")
			return 5
		}
		if *jsonOutput {
			encoder := json.NewEncoder(stdout)
			encoder.SetIndent("", "  ")
			if err := encoder.Encode(proof); err != nil {
				fmt.Fprintf(stderr, "输出 Wails 绑定证明失败: %v\n", err)
				return 1
			}
		} else {
			fmt.Fprintf(stdout, "Wails bindings: count=%d bound_methods=%d unbound_methods=%d missing_types=%d\n",
				len(proof.Bindings), len(proof.BoundMethods), len(proof.UnboundMethods), len(proof.MissingTypes))
		}
	}
	root := strings.TrimSpace(*produceRoot)
	if root != "" {
		proof, err := panicguard.ProveServiceProduceCoverage(root)
		if err != nil {
			fmt.Fprintf(stderr, "Produce 覆盖证明失败: %v\n", err)
			return 1
		}
		if *jsonOutput {
			encoder := json.NewEncoder(stdout)
			encoder.SetIndent("", "  ")
			if err := encoder.Encode(proof); err != nil {
				fmt.Fprintf(stderr, "输出 Produce 覆盖证明失败: %v\n", err)
				return 1
			}
		} else {
			fmt.Fprintf(stdout, "Produce coverage: methods=%d covered=%d missing=%d percent=%.2f\n",
				len(proof.Methods), len(proof.Covered), len(proof.Missing), proof.Percent)
		}
		if inventoryPath := strings.TrimSpace(*produceInventory); inventoryPath != "" {
			data, err := os.ReadFile(inventoryPath)
			if err != nil {
				fmt.Fprintf(stderr, "读取 Produce inventory 失败: %v\n", err)
				return 1
			}
			inventory, err := panicguard.DecodeProduceInventory(data)
			if err != nil {
				fmt.Fprintf(stderr, "解析 Produce inventory 失败: %v\n", err)
				return 1
			}
			if err := inventory.Enforce(proof); err != nil {
				fmt.Fprintf(stderr, "%v\n", err)
				return 6
			}
		}
	}
	return 0
}

func readCatalog(path string) (apicatalog.Catalog, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return apicatalog.Catalog{}, err
	}
	return apicatalog.Decode(data)
}

func readCallerUsages(paths []string) ([]apicatalog.CallerUsage, error) {
	usages := make([]apicatalog.CallerUsage, 0, len(paths))
	for _, path := range paths {
		data, err := os.ReadFile(path)
		if err != nil {
			return nil, err
		}
		usage, err := apicatalog.DecodeCallerUsage(data)
		if err != nil {
			return nil, err
		}
		usages = append(usages, usage)
	}
	return usages, nil
}

type repeatedFlag []string

func (flag *repeatedFlag) Set(value string) error {
	*flag = append(*flag, value)
	return nil
}

func (flag *repeatedFlag) String() string {
	return fmt.Sprint([]string(*flag))
}
