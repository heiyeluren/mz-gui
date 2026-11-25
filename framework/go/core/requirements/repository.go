package requirements

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

func (catalog Catalog) ValidateRepository(root string) error {
	absoluteRoot, err := filepath.Abs(root)
	if err != nil {
		return fmt.Errorf("%w: %v", ErrInvalidReference, err)
	}
	resolvedRoot, err := filepath.EvalSymlinks(absoluteRoot)
	if err != nil {
		return fmt.Errorf("%w: %v", ErrInvalidReference, err)
	}
	for _, requirement := range catalog.Requirements {
		if err := validateReferences(absoluteRoot, resolvedRoot, requirement.RequirementID, requirement.ImplementationRefs, true); err != nil {
			return err
		}
		if err := validateReferences(absoluteRoot, resolvedRoot, requirement.RequirementID, requirement.ContractRefs, true); err != nil {
			return err
		}
		for _, references := range [][]string{requirement.TestRefs, requirement.EvidenceRefs} {
			if err := validateReferences(absoluteRoot, resolvedRoot, requirement.RequirementID, references, false); err != nil {
				return err
			}
		}
	}
	return nil
}

func validateReferences(root string, resolvedRoot string, requirementID string, references []string, frameworkOnly bool) error {
	for _, reference := range references {
		clean := filepath.Clean(reference)
		if filepath.IsAbs(reference) || clean == "." || clean == ".." || strings.HasPrefix(clean, ".."+string(filepath.Separator)) {
			return fmt.Errorf("%w: %s -> %s", ErrInvalidReference, requirementID, reference)
		}
		normalized := filepath.ToSlash(clean)
		if frameworkOnly && (normalized == "apps" || strings.HasPrefix(normalized, "apps/")) {
			return fmt.Errorf("%w: %s -> %s", ErrApplicationCoupling, requirementID, reference)
		}
		absolute := filepath.Join(root, clean)
		relative, err := filepath.Rel(root, absolute)
		if err != nil || relative == ".." || strings.HasPrefix(relative, ".."+string(filepath.Separator)) {
			return fmt.Errorf("%w: %s -> %s", ErrInvalidReference, requirementID, reference)
		}
		if _, err := os.Stat(absolute); err != nil {
			return fmt.Errorf("%w: %s -> %s: %v", ErrInvalidReference, requirementID, reference, err)
		}
		resolved, err := filepath.EvalSymlinks(absolute)
		if err != nil {
			return fmt.Errorf("%w: %s -> %s: %v", ErrInvalidReference, requirementID, reference, err)
		}
		resolvedRelative, err := filepath.Rel(resolvedRoot, resolved)
		if err != nil || resolvedRelative == ".." || strings.HasPrefix(resolvedRelative, ".."+string(filepath.Separator)) {
			return fmt.Errorf("%w: %s -> %s", ErrInvalidReference, requirementID, reference)
		}
		resolvedNormalized := filepath.ToSlash(resolvedRelative)
		if frameworkOnly && (resolvedNormalized == "apps" || strings.HasPrefix(resolvedNormalized, "apps/")) {
			return fmt.Errorf("%w: %s -> %s", ErrApplicationCoupling, requirementID, reference)
		}
	}
	return nil
}
