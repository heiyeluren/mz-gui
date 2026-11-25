package remoteservice

import (
	"context"
	"io"
	"net"
	"net/http"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"time"
	"unicode"

	frameworkclock "github.com/heiyeluren/mz-gui/framework/go/core/clock"
)

const defaultPACMaxBytes = 256 * 1024

type PACAction string

const (
	PACActionDirect PACAction = "DIRECT"
	PACActionProxy  PACAction = "PROXY"
	PACActionSOCKS  PACAction = "SOCKS"
	PACActionSOCKS5 PACAction = "SOCKS5"
	PACActionHTTPS  PACAction = "HTTPS"
)

type PACDirective struct {
	Action PACAction
	Host   string
	Port   int
}

type PACFinder interface {
	FindProxyForURL(target *url.URL) (string, error)
}

type PACFinderFunc func(*url.URL) (string, error)

func (finder PACFinderFunc) FindProxyForURL(target *url.URL) (string, error) {
	if finder == nil {
		return "", ErrInvalidPAC
	}
	return finder(target)
}

func NewPACFinderFunc(finder func(*url.URL) (string, error)) PACFinder {
	return PACFinderFunc(finder)
}

func NewStaticPACFinder(decision string) PACFinder {
	return PACFinderFunc(func(*url.URL) (string, error) {
		return decision, nil
	})
}

type PACResolution struct {
	Direct     bool
	ProxyURL   string
	Decision   string
	Directives []PACDirective
}

type PACResolver struct {
	finder PACFinder
}

func NewPACResolver(finder PACFinder) (*PACResolver, error) {
	if finder == nil {
		return nil, ErrInvalidPAC
	}
	return &PACResolver{finder: finder}, nil
}

func (resolver *PACResolver) Resolve(target *url.URL) (PACResolution, error) {
	if resolver == nil || resolver.finder == nil || target == nil || target.Host == "" {
		return PACResolution{}, ErrInvalidPAC
	}
	if target.Scheme != "http" && target.Scheme != "https" {
		return PACResolution{}, ErrInvalidPAC
	}
	decision, err := resolver.finder.FindProxyForURL(target)
	if err != nil {
		return PACResolution{}, err
	}
	directives, err := ParsePACDecision(decision)
	if err != nil {
		return PACResolution{}, err
	}
	resolution := PACResolution{Decision: strings.TrimSpace(decision), Directives: directives}
	for _, directive := range directives {
		if directive.Action == PACActionDirect {
			resolution.Direct = true
			return resolution, nil
		}
		proxyURL, err := directive.proxyURL()
		if err != nil {
			return PACResolution{}, err
		}
		resolution.ProxyURL = proxyURL
		return resolution, nil
	}
	return PACResolution{}, ErrPACDecisionInvalid
}

func ParsePACDecision(raw string) ([]PACDirective, error) {
	normalized := strings.TrimSpace(raw)
	if normalized == "" {
		return nil, ErrPACDecisionInvalid
	}
	parts := strings.Split(normalized, ";")
	directives := make([]PACDirective, 0, len(parts))
	for _, part := range parts {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		directive, err := parsePACDirective(part)
		if err != nil {
			return nil, err
		}
		directives = append(directives, directive)
	}
	if len(directives) == 0 {
		return nil, ErrPACDecisionInvalid
	}
	return directives, nil
}

func ParseConstantPACScript(script string) (string, error) {
	body := stripPACComments(script)
	body = strings.TrimSpace(body)
	match := constantPACScriptPattern.FindStringSubmatch(body)
	if match == nil {
		return "", ErrPACScriptUnsupported
	}
	return match[1], nil
}

func WPADCandidates(dnsSuffix string) ([]string, error) {
	suffix := strings.ToLower(strings.TrimSpace(dnsSuffix))
	suffix = strings.TrimPrefix(suffix, ".")
	if suffix == "" || strings.ContainsAny(suffix, "/\\:@?&# ") || strings.Contains(suffix, "..") {
		return nil, ErrInvalidPAC
	}
	for _, label := range strings.Split(suffix, ".") {
		if label == "" || !dnsLabelPattern.MatchString(label) {
			return nil, ErrInvalidPAC
		}
	}
	return []string{"http://wpad." + suffix + "/wpad.dat"}, nil
}

type PACDocument struct {
	SourceURL string
	Body      string
	FetchedAt time.Time
}

type PACFetchOptions struct {
	HTTPClient *http.Client
	Timeout    time.Duration
	MaxBytes   int64
	Clock      frameworkclock.Clock
}

type PACFetcher struct {
	client   *http.Client
	timeout  time.Duration
	maxBytes int64
	clock    frameworkclock.Clock
}

func NewPACFetcher(options PACFetchOptions) (*PACFetcher, error) {
	if options.Timeout <= 0 {
		options.Timeout = 5 * time.Second
	}
	if options.MaxBytes <= 0 {
		options.MaxBytes = defaultPACMaxBytes
	}
	if options.Clock == nil {
		options.Clock = frameworkclock.System{}
	}
	client := options.HTTPClient
	if client == nil {
		client = &http.Client{
			Timeout: options.Timeout,
			CheckRedirect: func(request *http.Request, via []*http.Request) error {
				if request == nil || request.URL == nil || request.URL.User != nil {
					return ErrInvalidPAC
				}
				if request.URL.Scheme != "http" && request.URL.Scheme != "https" {
					return ErrInvalidPAC
				}
				if len(via) >= 5 {
					return ErrPACFetchFailed
				}
				return nil
			},
		}
	}
	return &PACFetcher{client: client, timeout: options.Timeout, maxBytes: options.MaxBytes, clock: options.Clock}, nil
}

func (fetcher *PACFetcher) Fetch(ctx context.Context, pacURL string) (PACDocument, error) {
	if fetcher == nil || fetcher.client == nil || ctx == nil {
		return PACDocument{}, ErrInvalidPAC
	}
	parsed, err := parsePACSourceURL(pacURL)
	if err != nil {
		return PACDocument{}, err
	}
	callContext, cancel := context.WithTimeout(ctx, fetcher.timeout)
	defer cancel()
	request, err := http.NewRequestWithContext(callContext, http.MethodGet, parsed.String(), nil)
	if err != nil {
		return PACDocument{}, err
	}
	response, err := fetcher.client.Do(request)
	if err != nil {
		return PACDocument{}, ErrPACFetchFailed
	}
	defer response.Body.Close()
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return PACDocument{}, ErrPACFetchFailed
	}
	limited := io.LimitReader(response.Body, fetcher.maxBytes+1)
	body, err := io.ReadAll(limited)
	if err != nil {
		return PACDocument{}, ErrPACFetchFailed
	}
	if int64(len(body)) > fetcher.maxBytes {
		return PACDocument{}, ErrPACFetchFailed
	}
	return PACDocument{
		SourceURL: parsed.String(),
		Body:      string(body),
		FetchedAt: fetcher.clock.Now(),
	}, nil
}

func parsePACDirective(raw string) (PACDirective, error) {
	fields := strings.Fields(raw)
	if len(fields) == 0 {
		return PACDirective{}, ErrPACDecisionInvalid
	}
	action := PACAction(strings.ToUpper(fields[0]))
	if action == PACActionDirect {
		if len(fields) != 1 {
			return PACDirective{}, ErrPACDecisionInvalid
		}
		return PACDirective{Action: PACActionDirect}, nil
	}
	if len(fields) != 2 || (action != PACActionProxy && action != PACActionSOCKS && action != PACActionSOCKS5 && action != PACActionHTTPS) {
		return PACDirective{}, ErrPACDecisionInvalid
	}
	host, port, err := parsePACHostPort(fields[1])
	if err != nil {
		return PACDirective{}, err
	}
	return PACDirective{Action: action, Host: host, Port: port}, nil
}

func parsePACHostPort(raw string) (string, int, error) {
	host, portText, err := net.SplitHostPort(raw)
	if err != nil {
		return "", 0, ErrPACDecisionInvalid
	}
	host = strings.TrimSpace(host)
	if host == "" || strings.ContainsAny(host, "/\\@ ?#") || net.ParseIP(host) == nil && !hostPattern.MatchString(host) {
		return "", 0, ErrPACDecisionInvalid
	}
	port, err := strconv.Atoi(portText)
	if err != nil || port < 1 || port > 65535 {
		return "", 0, ErrPACDecisionInvalid
	}
	return host, port, nil
}

func (directive PACDirective) proxyURL() (string, error) {
	scheme := ""
	switch directive.Action {
	case PACActionProxy:
		scheme = "http"
	case PACActionHTTPS:
		scheme = "https"
	case PACActionSOCKS, PACActionSOCKS5:
		scheme = "socks5"
	default:
		return "", ErrPACDecisionInvalid
	}
	return (&url.URL{Scheme: scheme, Host: net.JoinHostPort(directive.Host, strconv.Itoa(directive.Port))}).String(), nil
}

func parsePACSourceURL(rawURL string) (*url.URL, error) {
	parsed, err := url.Parse(strings.TrimSpace(rawURL))
	if err != nil || parsed == nil || parsed.Host == "" || parsed.User != nil {
		return nil, ErrInvalidPAC
	}
	if parsed.Scheme != "http" && parsed.Scheme != "https" {
		return nil, ErrInvalidPAC
	}
	return parsed, nil
}

func stripPACComments(script string) string {
	withoutBlock := blockCommentPattern.ReplaceAllString(script, " ")
	lines := strings.Split(withoutBlock, "\n")
	cleaned := make([]string, 0, len(lines))
	for _, line := range lines {
		if index := strings.Index(line, "//"); index >= 0 {
			line = line[:index]
		}
		cleaned = append(cleaned, line)
	}
	return strings.Map(func(r rune) rune {
		if unicode.IsSpace(r) && r != ' ' && r != '\n' {
			return ' '
		}
		return r
	}, strings.Join(cleaned, "\n"))
}

var (
	constantPACScriptPattern = regexp.MustCompile(`(?is)^function\s+FindProxyForURL\s*\(\s*url\s*,\s*host\s*\)\s*\{\s*return\s+["']([^"']+)["']\s*;\s*\}\s*$`)
	dnsLabelPattern          = regexp.MustCompile(`^[a-z0-9](?:[a-z0-9-]{0,61}[a-z0-9])?$`)
	hostPattern              = regexp.MustCompile(`^[A-Za-z0-9](?:[A-Za-z0-9.-]{0,251}[A-Za-z0-9])$`)
	blockCommentPattern      = regexp.MustCompile(`(?s)/\*.*?\*/`)
)
