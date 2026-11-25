package fault

import (
	"errors"
	"sync"
)

var ErrInvalidBinding = errors.New("Framework 错误映射无效")

type Binding struct {
	Match error
	Spec  Spec
}

type Mapper struct {
	mu       sync.RWMutex
	bindings []Binding
}

func NewMapper() *Mapper {
	return &Mapper{}
}

func (mapper *Mapper) Bind(match error, spec Spec) error {
	if mapper == nil || match == nil {
		return ErrInvalidBinding
	}
	spec.Cause = match
	if _, err := New(spec); err != nil {
		return ErrInvalidBinding
	}
	spec.Cause = nil
	mapper.mu.Lock()
	defer mapper.mu.Unlock()
	for _, binding := range mapper.bindings {
		if errors.Is(binding.Match, match) && errors.Is(match, binding.Match) {
			return ErrInvalidBinding
		}
	}
	mapper.bindings = append(mapper.bindings, Binding{Match: match, Spec: spec})
	return nil
}

func (mapper *Mapper) Produce(err error) *Error {
	if err == nil {
		return nil
	}
	var existing *Error
	if errors.As(err, &existing) {
		return existing
	}
	if mapper != nil {
		mapper.mu.RLock()
		bindings := append([]Binding(nil), mapper.bindings...)
		mapper.mu.RUnlock()
		for _, binding := range bindings {
			if errors.Is(err, binding.Match) {
				spec := binding.Spec
				spec.Cause = err
				failure, createErr := New(spec)
				if createErr != nil {
					return Normalize(err)
				}
				return failure
			}
		}
	}
	return Normalize(err)
}
