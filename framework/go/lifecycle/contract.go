package lifecycle

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

var reasonPattern = regexp.MustCompile("^[a-z][a-z0-9]*(?:\\.[a-z][a-z0-9_]*)+$")

//go:embed v1.json
var builtinContractJSON []byte

type ReasonDefinition struct {
	ReasonCode   string  `json:"reason_code"`
	TargetStates []State `json:"target_states"`
}

type Contract struct {
	SchemaRef     string             `json:"$schema"`
	SchemaVersion int                `json:"schema_version"`
	Release       string             `json:"release"`
	States        []State            `json:"states"`
	Reasons       []ReasonDefinition `json:"reasons"`

	states  map[State]struct{}
	reasons map[string]map[State]struct{}
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

func (contract Contract) Allows(reason string, target State) bool {
	targets, exists := contract.reasons[reason]
	if !exists {
		return false
	}
	_, exists = targets[target]
	return exists
}

func (contract *Contract) validate() error {
	version, err := model.ParseVersion(contract.Release)
	if contract.SchemaVersion != ContractSchemaVersion || err != nil || version.Major == 0 ||
		version.Prerelease != "" || version.Build != "" || len(contract.States) == 0 || len(contract.Reasons) == 0 {
		return ErrInvalidContract
	}
	contract.states = make(map[State]struct{}, len(contract.States))
	for _, state := range contract.States {
		if !state.Valid() {
			return ErrInvalidContract
		}
		if _, exists := contract.states[state]; exists {
			return ErrInvalidContract
		}
		contract.states[state] = struct{}{}
	}
	contract.reasons = make(map[string]map[State]struct{}, len(contract.Reasons))
	for _, definition := range contract.Reasons {
		if !reasonPattern.MatchString(definition.ReasonCode) || len(definition.TargetStates) == 0 {
			return ErrInvalidContract
		}
		if _, exists := contract.reasons[definition.ReasonCode]; exists {
			return ErrInvalidContract
		}
		targets := make(map[State]struct{}, len(definition.TargetStates))
		for _, state := range definition.TargetStates {
			if _, exists := contract.states[state]; !exists {
				return ErrInvalidContract
			}
			if _, exists := targets[state]; exists {
				return ErrInvalidContract
			}
			targets[state] = struct{}{}
		}
		contract.reasons[definition.ReasonCode] = targets
	}
	return nil
}
