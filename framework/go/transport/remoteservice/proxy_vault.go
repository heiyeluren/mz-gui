package remoteservice

import (
	"context"
	"encoding/json"
	"errors"
	"net/url"
	"strconv"
	"strings"
)

type ProxyCredentialStore interface {
	Get(context.Context, string) (string, error)
	Set(context.Context, string, string) error
	Delete(context.Context, string) error
}

type ProxyCredentialVaultOptions struct {
	Store     ProxyCredentialStore
	Namespace string
}

type ProxyCredentialRef struct {
	Host     string `json:"host"`
	Port     int    `json:"port"`
	Realm    string `json:"realm,omitempty"`
	Username string `json:"username"`
	Key      string `json:"key"`
}

func (ref ProxyCredentialRef) String() string {
	if ref.Key == "" {
		return "proxy-credential:<invalid>"
	}
	return "proxy-credential:" + ref.Key
}

type ProxyCredential struct {
	Username string
	Password string
	Scheme   string
}

type ProxyCredentialVault struct {
	store     ProxyCredentialStore
	namespace string
}

func NewProxyCredentialVault(options ProxyCredentialVaultOptions) (*ProxyCredentialVault, error) {
	if options.Store == nil || !validOAuthCredentialPart(options.Namespace) {
		return nil, ErrInvalidProxyCredentialRef
	}
	return &ProxyCredentialVault{store: options.Store, namespace: options.Namespace}, nil
}

func (vault *ProxyCredentialVault) Save(ctx context.Context, ref ProxyCredentialRef, credential ProxyCredential) (ProxyCredentialRef, error) {
	if vault == nil || vault.store == nil || ctx == nil {
		return ProxyCredentialRef{}, ErrInvalidProxyCredentialRef
	}
	resolved, err := vault.resolveRef(ref)
	if err != nil {
		return ProxyCredentialRef{}, err
	}
	if credential.Username == "" || credential.Username != resolved.Username || credential.Password == "" {
		return ProxyCredentialRef{}, ErrInvalidProxyCredentialRef
	}
	scheme, err := normalizeProxyAuthScheme(credential.Scheme)
	if err != nil {
		return ProxyCredentialRef{}, err
	}
	payload, err := json.Marshal(proxyCredentialPayload{
		Username: resolved.Username, Password: credential.Password, Scheme: scheme,
	})
	if err != nil {
		return ProxyCredentialRef{}, err
	}
	if err := vault.store.Set(ctx, resolved.Key, string(payload)); err != nil {
		return ProxyCredentialRef{}, sanitizeProxyCredentialStoreError(err)
	}
	return resolved, nil
}

func (vault *ProxyCredentialVault) Load(ctx context.Context, ref ProxyCredentialRef) (ProxyCredential, error) {
	if vault == nil || vault.store == nil || ctx == nil {
		return ProxyCredential{}, ErrInvalidProxyCredentialRef
	}
	resolved, err := vault.resolveRef(ref)
	if err != nil {
		return ProxyCredential{}, err
	}
	value, err := vault.store.Get(ctx, resolved.Key)
	if err != nil {
		return ProxyCredential{}, sanitizeProxyCredentialStoreError(err)
	}
	var payload proxyCredentialPayload
	if err := json.Unmarshal([]byte(value), &payload); err != nil || payload.Username == "" || payload.Password == "" {
		return ProxyCredential{}, ErrProxyCredentialStoreUnavailable
	}
	return ProxyCredential{Username: payload.Username, Password: payload.Password, Scheme: payload.Scheme}, nil
}

func (vault *ProxyCredentialVault) Delete(ctx context.Context, ref ProxyCredentialRef) error {
	if vault == nil || vault.store == nil || ctx == nil {
		return ErrInvalidProxyCredentialRef
	}
	resolved, err := vault.resolveRef(ref)
	if err != nil {
		return err
	}
	if err := vault.store.Delete(ctx, resolved.Key); err != nil {
		return sanitizeProxyCredentialStoreError(err)
	}
	return nil
}

func (vault *ProxyCredentialVault) ProxyURL(ctx context.Context, proxyURL string, ref ProxyCredentialRef) (*url.URL, error) {
	if vault == nil {
		return nil, ErrInvalidProxyCredentialRef
	}
	parsed, err := parseProxyDiagnosticURL(proxyURL)
	if err != nil {
		return nil, err
	}
	resolved, err := vault.resolveRef(ref)
	if err != nil {
		return nil, err
	}
	if !sameProxyEndpoint(parsed, resolved) {
		return nil, ErrInvalidProxyCredentialRef
	}
	credential, err := vault.Load(ctx, resolved)
	if err != nil {
		return nil, err
	}
	parsed.User = url.UserPassword(credential.Username, credential.Password)
	return parsed, nil
}

func (vault *ProxyCredentialVault) resolveRef(ref ProxyCredentialRef) (ProxyCredentialRef, error) {
	if !hostPattern.MatchString(ref.Host) || strings.ToLower(ref.Host) != ref.Host || strings.Contains(ref.Host, "@") {
		return ProxyCredentialRef{}, ErrInvalidProxyCredentialRef
	}
	if ref.Port < 1 || ref.Port > 65535 || !validOAuthCredentialPart(ref.Username) {
		return ProxyCredentialRef{}, ErrInvalidProxyCredentialRef
	}
	realm := strings.TrimSpace(ref.Realm)
	if realm == "" {
		realm = "default"
	}
	if !validOAuthCredentialPart(realm) {
		return ProxyCredentialRef{}, ErrInvalidProxyCredentialRef
	}
	expectedKey := strings.Join([]string{vault.namespace, ref.Host, strconv.Itoa(ref.Port), realm, ref.Username}, "/")
	if ref.Key != "" && ref.Key != expectedKey {
		return ProxyCredentialRef{}, ErrInvalidProxyCredentialRef
	}
	ref.Realm = realm
	ref.Key = expectedKey
	return ref, nil
}

type proxyCredentialPayload struct {
	Username string `json:"username"`
	Password string `json:"password"`
	Scheme   string `json:"scheme,omitempty"`
}

func normalizeProxyAuthScheme(scheme string) (string, error) {
	scheme = strings.TrimSpace(scheme)
	if scheme == "" {
		return "Basic", nil
	}
	switch strings.ToLower(scheme) {
	case "basic":
		return "Basic", nil
	case "digest":
		return "Digest", nil
	case "negotiate":
		return "Negotiate", nil
	default:
		return "", ErrInvalidProxyCredentialRef
	}
}

func sameProxyEndpoint(parsed *url.URL, ref ProxyCredentialRef) bool {
	if parsed == nil {
		return false
	}
	host := parsed.Hostname()
	port := parsed.Port()
	if port == "" {
		if parsed.Scheme == "https" {
			port = "443"
		} else {
			port = "80"
		}
	}
	return host == ref.Host && port == strconv.Itoa(ref.Port)
}

func sanitizeProxyCredentialStoreError(err error) error {
	if err == nil {
		return nil
	}
	if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		return err
	}
	if errors.Is(err, ErrProxyCredentialNotFound) {
		return ErrProxyCredentialNotFound
	}
	return ErrProxyCredentialStoreUnavailable
}
