package webpolicy

import (
	"errors"
	"fmt"
	"net"
	"net/url"
	"sort"
	"strings"
)

const (
	ReasonAllowed       = "webpolicy.allowed"
	ReasonInvalidURL    = "webpolicy.invalid_url"
	ReasonSchemeDenied  = "webpolicy.scheme_denied"
	ReasonHostDenied    = "webpolicy.host_denied"
	ReasonPolicyInvalid = "webpolicy.policy_invalid"
)

type Environment string

const (
	EnvironmentDevelopment Environment = "development"
	EnvironmentProduction  Environment = "production"
)

type Options struct {
	InternalOrigin  string
	NavigationRules []URLRule
	ResourceRules   []URLRule
	CSPDirectives   map[string][]string
	DevTools        DevToolsPolicy
}

type Policy struct {
	internalOrigin origin
	navigation     []URLRule
	resources      []URLRule
	csp            map[string][]string
	devtools       DevToolsPolicy
}

type URLRule struct {
	ID              string
	Scheme          string
	Host            string
	AllowSubdomains bool
}

type DevToolsPolicy struct {
	Enabled             bool
	AllowedEnvironments []Environment
}

type Decision struct {
	Allowed     bool
	ReasonCode  string
	MatchedRule string
}

type origin struct {
	scheme string
	host   string
}

func NewStrictPolicy(options Options) (Policy, error) {
	internalOrigin, err := parseOrigin(options.InternalOrigin)
	if err != nil {
		return Policy{}, err
	}
	policy := Policy{
		internalOrigin: internalOrigin,
		navigation:     append([]URLRule(nil), options.NavigationRules...),
		resources:      append([]URLRule(nil), options.ResourceRules...),
		csp:            cloneDirectives(options.CSPDirectives),
		devtools:       options.DevTools,
	}
	if len(policy.csp) == 0 {
		policy.csp = defaultCSPDirectives()
	}
	if err := policy.Validate(); err != nil {
		return Policy{}, err
	}
	return policy, nil
}

func (policy Policy) Validate() error {
	if policy.internalOrigin.scheme == "" || policy.internalOrigin.host == "" {
		return errors.New("内部来源不能为空")
	}
	for _, rule := range append(append([]URLRule(nil), policy.navigation...), policy.resources...) {
		if err := rule.Validate(); err != nil {
			return err
		}
	}
	for name, values := range policy.csp {
		if !validCSPToken(name) || len(values) == 0 {
			return fmt.Errorf("CSP directive 无效")
		}
		for _, value := range values {
			if !validCSPValue(value) {
				return fmt.Errorf("CSP directive 值无效")
			}
		}
	}
	return nil
}

func (policy Policy) EvaluateNavigation(rawURL string) Decision {
	return policy.evaluate(rawURL, policy.navigation)
}

func (policy Policy) EvaluateResource(rawURL string) Decision {
	return policy.evaluate(rawURL, policy.resources)
}

func (policy Policy) CSPHeader() (string, error) {
	if err := policy.Validate(); err != nil {
		return "", err
	}
	names := make([]string, 0, len(policy.csp))
	for name := range policy.csp {
		names = append(names, name)
	}
	sort.Strings(names)
	parts := make([]string, 0, len(names))
	for _, name := range names {
		values := append([]string(nil), policy.csp[name]...)
		sort.Strings(values)
		parts = append(parts, name+" "+strings.Join(values, " "))
	}
	return strings.Join(parts, "; "), nil
}

func (policy Policy) AllowsDevTools(environment Environment) bool {
	if !policy.devtools.Enabled {
		return false
	}
	for _, allowed := range policy.devtools.AllowedEnvironments {
		if allowed == environment {
			return true
		}
	}
	return false
}

func (policy Policy) evaluate(rawURL string, rules []URLRule) Decision {
	parsed, err := url.Parse(rawURL)
	if err != nil || parsed.Scheme == "" {
		return Decision{ReasonCode: ReasonInvalidURL}
	}
	scheme := strings.ToLower(parsed.Scheme)
	host := normalizeHost(parsed.Hostname())
	if scheme == policy.internalOrigin.scheme && host == policy.internalOrigin.host {
		return Decision{Allowed: true, ReasonCode: ReasonAllowed, MatchedRule: "internal-origin"}
	}
	if !isNetworkScheme(scheme) {
		return Decision{ReasonCode: ReasonSchemeDenied}
	}
	if host == "" {
		return Decision{ReasonCode: ReasonHostDenied}
	}
	for _, rule := range rules {
		if rule.matches(scheme, host) {
			return Decision{Allowed: true, ReasonCode: ReasonAllowed, MatchedRule: rule.ID}
		}
	}
	return Decision{ReasonCode: ReasonHostDenied}
}

func (rule URLRule) Validate() error {
	if rule.ID == "" || rule.Scheme == "" || rule.Host == "" {
		return errors.New("URL 规则字段不能为空")
	}
	scheme := strings.ToLower(rule.Scheme)
	host := normalizeHost(rule.Host)
	if !isNetworkScheme(scheme) || strings.ContainsAny(scheme, "*:/") {
		return errors.New("URL 规则 scheme 无效")
	}
	if host == "*" || strings.Contains(host, "*") || net.ParseIP(host) == nil && strings.ContainsAny(host, "/:") {
		return errors.New("URL 规则 host 无效")
	}
	return nil
}

func (rule URLRule) matches(scheme string, host string) bool {
	ruleScheme := strings.ToLower(rule.Scheme)
	ruleHost := normalizeHost(rule.Host)
	if scheme != ruleScheme {
		return false
	}
	if host == ruleHost {
		return true
	}
	return rule.AllowSubdomains && strings.HasSuffix(host, "."+ruleHost)
}

func parseOrigin(value string) (origin, error) {
	parsed, err := url.Parse(value)
	if err != nil || parsed.Scheme == "" || parsed.Hostname() == "" {
		return origin{}, errors.New("内部来源无效")
	}
	scheme := strings.ToLower(parsed.Scheme)
	if !validCustomScheme(scheme) {
		return origin{}, errors.New("内部来源 scheme 无效")
	}
	return origin{scheme: scheme, host: normalizeHost(parsed.Hostname())}, nil
}

func cloneDirectives(input map[string][]string) map[string][]string {
	if len(input) == 0 {
		return nil
	}
	cloned := make(map[string][]string, len(input))
	for name, values := range input {
		cloned[name] = append([]string(nil), values...)
	}
	return cloned
}

func defaultCSPDirectives() map[string][]string {
	return map[string][]string{
		"base-uri":        {"'self'"},
		"connect-src":     {"'self'"},
		"default-src":     {"'self'"},
		"frame-ancestors": {"'none'"},
		"img-src":         {"'self'", "data:"},
		"object-src":      {"'none'"},
		"script-src":      {"'self'"},
		"style-src":       {"'self'"},
	}
}

func normalizeHost(host string) string {
	return strings.ToLower(strings.TrimSuffix(host, "."))
}

func isNetworkScheme(scheme string) bool {
	return scheme == "https" || scheme == "wss"
}

func validCustomScheme(scheme string) bool {
	if scheme == "" || strings.ContainsAny(scheme, "*:/") {
		return false
	}
	for _, char := range scheme {
		if (char >= 'a' && char <= 'z') || (char >= '0' && char <= '9') || char == '+' || char == '-' || char == '.' {
			continue
		}
		return false
	}
	return true
}

func validCSPToken(value string) bool {
	if value == "" {
		return false
	}
	for _, char := range value {
		if (char >= 'a' && char <= 'z') || (char >= '0' && char <= '9') || char == '-' {
			continue
		}
		return false
	}
	return true
}

func validCSPValue(value string) bool {
	return value != "" && !strings.ContainsAny(value, ";\n\r")
}
