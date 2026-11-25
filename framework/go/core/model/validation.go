package model

import (
	"errors"
	"regexp"
)

var (
	ErrInvalidValidationIssue = errors.New("Framework 校验问题无效")
	fieldPathPattern          = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9_.\[\]-]{0,255}$`)
	keyPattern                = regexp.MustCompile(`^[a-z][a-z0-9_]{0,63}$`)
	messageKeyPattern         = regexp.MustCompile(`^[a-z][a-z0-9_]*(?:\.[a-z][a-z0-9_]*)+$`)
	sensitiveKeyPattern       = regexp.MustCompile(`(?i)(token|secret|password|cookie|authorization|credential|private_key|api_key|path|url|command|stderr|stack)`)
	safeValuePattern          = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._:-]{0,255}$`)
)

type ValidationIssue struct {
	Path       string            `json:"path"`
	Rule       string            `json:"rule"`
	MessageKey string            `json:"message_key"`
	Params     map[string]string `json:"params,omitempty"`
}

func (issue ValidationIssue) Validate() error {
	if !fieldPathPattern.MatchString(issue.Path) || !keyPattern.MatchString(issue.Rule) || !messageKeyPattern.MatchString(issue.MessageKey) || len(issue.Params) > 16 {
		return ErrInvalidValidationIssue
	}
	for key, value := range issue.Params {
		if !keyPattern.MatchString(key) || sensitiveKeyPattern.MatchString(key) || !safeValuePattern.MatchString(value) {
			return ErrInvalidValidationIssue
		}
	}
	return nil
}
