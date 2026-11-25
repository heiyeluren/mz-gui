package managedprocess

import (
	"bytes"
	_ "embed"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"regexp"

	"github.com/heiyeluren/mz-gui/framework/go/core/model"
)

const ContractSchemaVersion = 1

var (
	ErrInvalidContract = errors.New("受管进程契约无效")
	reasonCodePattern  = regexp.MustCompile(`^[a-z][a-z0-9]*(?:\.[a-z][a-z0-9_]*)+$`)
)

//go:embed v1.json
var builtinContractJSON []byte

type ReasonDefinition struct {
	ReasonCode   string      `json:"reason_code"`
	EventKinds   []EventKind `json:"event_kinds"`
	TargetStates []State     `json:"target_states"`
}

type Contract struct {
	SchemaRef       string             `json:"$schema"`
	SchemaVersion   int                `json:"schema_version"`
	Release         string             `json:"release"`
	States          []State            `json:"states"`
	RestartPolicies []RestartPolicy    `json:"restart_policies"`
	ProbePhases     []ProbePhase       `json:"probe_phases"`
	ProbeStatuses   []ProbeStatus      `json:"probe_statuses"`
	EventKinds      []EventKind        `json:"event_kinds"`
	Reasons         []ReasonDefinition `json:"reasons"`

	states   map[State]struct{}
	policies map[RestartPolicy]struct{}
	phases   map[ProbePhase]struct{}
	statuses map[ProbeStatus]struct{}
	kinds    map[EventKind]struct{}
	reasons  map[string]map[EventKind]map[State]struct{}
}

func BuiltinContract() (Contract, error) {
	return DecodeContract(builtinContractJSON)
}

func DecodeContract(data []byte) (Contract, error) {
	var contract Contract
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&contract); err != nil {
		return Contract{}, fmt.Errorf("%w: %v", ErrInvalidContract, err)
	}
	var trailing any
	if err := decoder.Decode(&trailing); !errors.Is(err, io.EOF) {
		return Contract{}, fmt.Errorf("%w: JSON 包含额外内容", ErrInvalidContract)
	}
	if err := contract.validate(); err != nil {
		return Contract{}, err
	}
	return contract, nil
}

func (contract Contract) HasState(state State) bool {
	_, exists := contract.states[state]
	return exists
}

func (contract Contract) HasRestartPolicy(policy RestartPolicy) bool {
	_, exists := contract.policies[policy]
	return exists
}

func (contract Contract) HasEventKind(kind EventKind) bool {
	_, exists := contract.kinds[kind]
	return exists
}

func (contract Contract) HasProbePhase(phase ProbePhase) bool {
	_, exists := contract.phases[phase]
	return exists
}

func (contract Contract) HasProbeStatus(status ProbeStatus) bool {
	_, exists := contract.statuses[status]
	return exists
}

func (contract Contract) Allows(reason string, kind EventKind, state State) bool {
	kinds, exists := contract.reasons[reason]
	if !exists {
		return false
	}
	states, exists := kinds[kind]
	if !exists {
		return false
	}
	_, exists = states[state]
	return exists
}

func (contract *Contract) validate() error {
	version, err := model.ParseVersion(contract.Release)
	if contract.SchemaRef != "./v1.schema.json" || contract.SchemaVersion != ContractSchemaVersion || err != nil || version.Major == 0 ||
		version.Prerelease != "" || version.Build != "" || len(contract.States) == 0 ||
		len(contract.RestartPolicies) == 0 || len(contract.ProbePhases) == 0 || len(contract.ProbeStatuses) == 0 ||
		len(contract.EventKinds) == 0 || len(contract.Reasons) == 0 {
		return ErrInvalidContract
	}
	contract.states = make(map[State]struct{}, len(contract.States))
	for _, state := range contract.States {
		if !state.Valid() || addDuplicate(contract.states, state) {
			return ErrInvalidContract
		}
	}
	contract.policies = make(map[RestartPolicy]struct{}, len(contract.RestartPolicies))
	for _, policy := range contract.RestartPolicies {
		if !policy.Valid() || addDuplicate(contract.policies, policy) {
			return ErrInvalidContract
		}
	}
	contract.kinds = make(map[EventKind]struct{}, len(contract.EventKinds))
	contract.phases = make(map[ProbePhase]struct{}, len(contract.ProbePhases))
	for _, phase := range contract.ProbePhases {
		if !phase.Valid() || addDuplicate(contract.phases, phase) {
			return ErrInvalidContract
		}
	}
	contract.statuses = make(map[ProbeStatus]struct{}, len(contract.ProbeStatuses))
	for _, status := range contract.ProbeStatuses {
		if !status.Valid() || addDuplicate(contract.statuses, status) {
			return ErrInvalidContract
		}
	}
	for _, kind := range contract.EventKinds {
		if !kind.Valid() || addDuplicate(contract.kinds, kind) {
			return ErrInvalidContract
		}
	}
	contract.reasons = make(map[string]map[EventKind]map[State]struct{}, len(contract.Reasons))
	for _, definition := range contract.Reasons {
		if !reasonCodePattern.MatchString(definition.ReasonCode) || len(definition.EventKinds) == 0 || len(definition.TargetStates) == 0 {
			return ErrInvalidContract
		}
		if _, exists := contract.reasons[definition.ReasonCode]; exists {
			return ErrInvalidContract
		}
		allowed := make(map[EventKind]map[State]struct{}, len(definition.EventKinds))
		for _, kind := range definition.EventKinds {
			if _, exists := contract.kinds[kind]; !exists {
				return ErrInvalidContract
			}
			if _, exists := allowed[kind]; exists {
				return ErrInvalidContract
			}
			states := make(map[State]struct{}, len(definition.TargetStates))
			for _, state := range definition.TargetStates {
				if _, exists := contract.states[state]; !exists || addDuplicate(states, state) {
					return ErrInvalidContract
				}
			}
			allowed[kind] = states
		}
		contract.reasons[definition.ReasonCode] = allowed
	}
	return nil
}

func addDuplicate[T comparable](values map[T]struct{}, value T) bool {
	if _, exists := values[value]; exists {
		return true
	}
	values[value] = struct{}{}
	return false
}
