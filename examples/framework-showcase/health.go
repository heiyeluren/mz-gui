package main

import (
	"fmt"

	frameworkclock "github.com/heiyeluren/mz-gui/framework/go/core/clock"
	frameworkhealth "github.com/heiyeluren/mz-gui/framework/go/core/health"
	"github.com/heiyeluren/mz-gui/framework/go/core/model"
)

func newShowcaseHealth(observedAt model.Timestamp, clock frameworkclock.Clock) (*frameworkhealth.Registry, *frameworkhealth.PanicMarker, error) {
	registry, err := frameworkhealth.NewBuiltinRegistry(observedAt)
	if err != nil {
		return nil, nil, fmt.Errorf("创建 showcase 内置健康 registry: %w", err)
	}
	marker, err := frameworkhealth.NewServicePanicMarker(registry, clock)
	if err != nil {
		return nil, nil, fmt.Errorf("绑定 showcase service panic 标记: %w", err)
	}
	return registry, marker, nil
}
