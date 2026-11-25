package testkit

import (
	"fmt"
)

type SoakResult struct {
	Iterations int
	Failures   int
}

func RunSoak(iterations int, fn func(int) error) (SoakResult, error) {
	if iterations < 1 || fn == nil {
		return SoakResult{}, fmt.Errorf("soak 参数无效")
	}
	result := SoakResult{Iterations: iterations}
	for index := 0; index < iterations; index++ {
		if err := fn(index); err != nil {
			result.Failures++
		}
	}
	if result.Failures > 0 {
		return result, fmt.Errorf("soak 失败 %d/%d", result.Failures, result.Iterations)
	}
	return result, nil
}
