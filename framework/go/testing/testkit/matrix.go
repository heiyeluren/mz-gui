package testkit

import (
	"fmt"
	"sort"
	"strings"
)

type MatrixCase struct {
	Name string
	Run  func(*FaultInjector) error
}

type MatrixResult struct {
	Cases    int
	Passed   int
	Failures []string
}

func RunNightMatrix(cases []MatrixCase) (MatrixResult, error) {
	if len(cases) == 0 {
		return MatrixResult{}, fmt.Errorf("夜间矩阵用例无效")
	}
	seen := make(map[string]struct{}, len(cases))
	result := MatrixResult{Cases: len(cases)}
	for _, item := range cases {
		name := strings.TrimSpace(item.Name)
		if name == "" || item.Run == nil {
			return MatrixResult{}, fmt.Errorf("夜间矩阵用例无效")
		}
		if _, exists := seen[name]; exists {
			return MatrixResult{}, fmt.Errorf("夜间矩阵用例重复: %s", name)
		}
		seen[name] = struct{}{}
		if err := item.Run(NewFaultInjector()); err != nil {
			result.Failures = append(result.Failures, name+": "+err.Error())
			continue
		}
		result.Passed++
	}
	sort.Strings(result.Failures)
	if len(result.Failures) > 0 {
		return result, fmt.Errorf("夜间矩阵失败 %d/%d", len(result.Failures), result.Cases)
	}
	return result, nil
}
