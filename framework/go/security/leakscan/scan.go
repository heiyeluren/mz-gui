package leakscan

import (
	"regexp"
	"strings"
)

type Finding struct {
	Kind   string
	Offset int
}

var secretPatterns = []*regexp.Regexp{
	regexp.MustCompile(`(?i)token\s*=\s*\S+`),
	regexp.MustCompile(`(?i)password\s*=\s*\S+`),
	regexp.MustCompile(`-----BEGIN [A-Z ]*PRIVATE KEY-----`),
	regexp.MustCompile(`(?i)api[_-]?key\s*=\s*\S+`),
}

func Scan(text string) []Finding {
	if strings.TrimSpace(text) == "" {
		return nil
	}
	findings := make([]Finding, 0)
	for _, pattern := range secretPatterns {
		for _, index := range pattern.FindAllStringIndex(text, -1) {
			findings = append(findings, Finding{Kind: pattern.String(), Offset: index[0]})
		}
	}
	return findings
}
