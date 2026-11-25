package apicatalog

import "fmt"

type ChangeKind string

const (
	ChangeAdded              ChangeKind = "added"
	ChangeRemoved            ChangeKind = "removed"
	ChangeSignatureChanged   ChangeKind = "signature-changed"
	ChangeStabilityRegressed ChangeKind = "stability-regressed"
	ChangeDeprecatedRemoved  ChangeKind = "deprecated-removed"
)

type Change struct {
	APIID    string     `json:"api_id"`
	Kind     ChangeKind `json:"kind"`
	Breaking bool       `json:"breaking"`
	Message  string     `json:"message"`
}

type Diff struct {
	OldRevision uint64   `json:"old_revision"`
	NewRevision uint64   `json:"new_revision"`
	Breaking    []Change `json:"breaking"`
	Warnings    []Change `json:"warnings"`
	Compatible  []Change `json:"compatible"`
}

func Compare(oldCatalog, newCatalog Catalog) Diff {
	result := Diff{OldRevision: oldCatalog.CatalogRevision, NewRevision: newCatalog.CatalogRevision}
	for _, oldAPI := range oldCatalog.APIs {
		newAPI, exists := newCatalog.Lookup(oldAPI.APIID)
		if !exists {
			change := Change{APIID: oldAPI.APIID, Message: fmt.Sprintf("%s 已删除", oldAPI.APIID)}
			if oldAPI.Stability == StabilityDeprecated {
				change.Kind = ChangeDeprecatedRemoved
				result.Warnings = append(result.Warnings, change)
			} else if protectedStability(oldAPI.Stability) {
				change.Kind = ChangeRemoved
				change.Breaking = true
				result.Breaking = append(result.Breaking, change)
			} else {
				change.Kind = ChangeRemoved
				result.Compatible = append(result.Compatible, change)
			}
			continue
		}
		if oldAPI.SignatureSHA256 != newAPI.SignatureSHA256 && protectedStability(oldAPI.Stability) {
			result.Breaking = append(result.Breaking, Change{
				APIID: oldAPI.APIID, Kind: ChangeSignatureChanged, Breaking: true,
				Message: fmt.Sprintf("%s 签名摘要变化", oldAPI.APIID),
			})
		}
		if stabilityRank(newAPI.Stability) < stabilityRank(oldAPI.Stability) {
			result.Breaking = append(result.Breaking, Change{
				APIID: oldAPI.APIID, Kind: ChangeStabilityRegressed, Breaking: true,
				Message: fmt.Sprintf("%s 稳定性从 %s 降级为 %s", oldAPI.APIID, oldAPI.Stability, newAPI.Stability),
			})
		}
	}
	for _, newAPI := range newCatalog.APIs {
		if _, exists := oldCatalog.Lookup(newAPI.APIID); !exists {
			result.Compatible = append(result.Compatible, Change{
				APIID: newAPI.APIID, Kind: ChangeAdded, Message: fmt.Sprintf("%s 已新增", newAPI.APIID),
			})
		}
	}
	return result
}

func (diff Diff) HasBreaking() bool {
	return len(diff.Breaking) > 0
}

func protectedStability(stability Stability) bool {
	return stability == StabilityCandidate || stability == StabilityStable
}

func stabilityRank(stability Stability) int {
	switch stability {
	case StabilityExperimental:
		return 1
	case StabilityCandidate:
		return 2
	case StabilityStable:
		return 3
	case StabilityDeprecated:
		return 4
	default:
		return 0
	}
}
