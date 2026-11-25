package remoteservice

import (
	"context"
	"encoding/json"
	"errors"
	"regexp"
	"strings"
	"time"
)

var oauthCredentialPartPattern = regexp.MustCompile(`^[a-z0-9][a-z0-9_-]{0,62}$`)

type OAuthTokenCredentialStore interface {
	Get(context.Context, string) (string, error)
	Set(context.Context, string, string) error
	Delete(context.Context, string) error
}

type OAuthTokenVaultOptions struct {
	Store     OAuthTokenCredentialStore
	Namespace string
}

type OAuthTokenCredentialRef struct {
	Provider string `json:"provider"`
	Account  string `json:"account"`
	Key      string `json:"key"`
}

func (ref OAuthTokenCredentialRef) String() string {
	if ref.Key == "" {
		return "oauth-token:<invalid>"
	}
	return "oauth-token:" + ref.Key
}

type OAuthTokenVault struct {
	store     OAuthTokenCredentialStore
	namespace string
}

func NewOAuthTokenVault(options OAuthTokenVaultOptions) (*OAuthTokenVault, error) {
	if options.Store == nil || !validOAuthCredentialPart(options.Namespace) {
		return nil, ErrInvalidOAuthCredentialRef
	}
	return &OAuthTokenVault{store: options.Store, namespace: options.Namespace}, nil
}

func (vault *OAuthTokenVault) Save(ctx context.Context, ref OAuthTokenCredentialRef, token OAuthToken) (OAuthTokenCredentialRef, error) {
	if vault == nil || vault.store == nil || ctx == nil {
		return OAuthTokenCredentialRef{}, ErrInvalidOAuthCredentialRef
	}
	resolved, err := vault.resolveRef(ref)
	if err != nil {
		return OAuthTokenCredentialRef{}, err
	}
	if token.AccessToken == "" {
		return OAuthTokenCredentialRef{}, ErrOAuthFailed
	}
	payload, err := json.Marshal(oauthTokenCredentialPayload{
		AccessToken: token.AccessToken, RefreshToken: token.RefreshToken, TokenType: token.TokenType,
		Scope: token.Scope, ExpiresInSeconds: int64(token.ExpiresIn.Seconds()),
	})
	if err != nil {
		return OAuthTokenCredentialRef{}, err
	}
	if err := vault.store.Set(ctx, resolved.Key, string(payload)); err != nil {
		return OAuthTokenCredentialRef{}, sanitizeOAuthCredentialStoreError(err)
	}
	return resolved, nil
}

func (vault *OAuthTokenVault) Load(ctx context.Context, ref OAuthTokenCredentialRef) (OAuthToken, error) {
	if vault == nil || vault.store == nil || ctx == nil {
		return OAuthToken{}, ErrInvalidOAuthCredentialRef
	}
	resolved, err := vault.resolveRef(ref)
	if err != nil {
		return OAuthToken{}, err
	}
	value, err := vault.store.Get(ctx, resolved.Key)
	if err != nil {
		return OAuthToken{}, sanitizeOAuthCredentialStoreError(err)
	}
	var payload oauthTokenCredentialPayload
	if err := json.Unmarshal([]byte(value), &payload); err != nil {
		return OAuthToken{}, ErrOAuthCredentialStoreUnavailable
	}
	return OAuthToken{
		AccessToken: payload.AccessToken, RefreshToken: payload.RefreshToken, TokenType: payload.TokenType,
		Scope: payload.Scope, ExpiresIn: secondsDuration(payload.ExpiresInSeconds),
	}, nil
}

func (vault *OAuthTokenVault) Delete(ctx context.Context, ref OAuthTokenCredentialRef) error {
	if vault == nil || vault.store == nil || ctx == nil {
		return ErrInvalidOAuthCredentialRef
	}
	resolved, err := vault.resolveRef(ref)
	if err != nil {
		return err
	}
	if err := vault.store.Delete(ctx, resolved.Key); err != nil {
		return sanitizeOAuthCredentialStoreError(err)
	}
	return nil
}

func (vault *OAuthTokenVault) resolveRef(ref OAuthTokenCredentialRef) (OAuthTokenCredentialRef, error) {
	if !validOAuthCredentialPart(ref.Provider) || !validOAuthCredentialPart(ref.Account) {
		return OAuthTokenCredentialRef{}, ErrInvalidOAuthCredentialRef
	}
	expectedKey := strings.Join([]string{vault.namespace, ref.Provider, ref.Account}, "/")
	if ref.Key != "" && ref.Key != expectedKey {
		return OAuthTokenCredentialRef{}, ErrInvalidOAuthCredentialRef
	}
	ref.Key = expectedKey
	return ref, nil
}

type oauthTokenCredentialPayload struct {
	AccessToken      string `json:"access_token"`
	RefreshToken     string `json:"refresh_token,omitempty"`
	TokenType        string `json:"token_type,omitempty"`
	Scope            string `json:"scope,omitempty"`
	ExpiresInSeconds int64  `json:"expires_in,omitempty"`
}

func validOAuthCredentialPart(value string) bool {
	return oauthCredentialPartPattern.MatchString(value)
}

func sanitizeOAuthCredentialStoreError(err error) error {
	if err == nil {
		return nil
	}
	if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		return err
	}
	if errors.Is(err, ErrOAuthCredentialNotFound) {
		return ErrOAuthCredentialNotFound
	}
	return ErrOAuthCredentialStoreUnavailable
}

func secondsDuration(seconds int64) time.Duration {
	if seconds <= 0 {
		return 0
	}
	return time.Duration(seconds) * time.Second
}
