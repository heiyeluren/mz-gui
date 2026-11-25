package remoteservice

import (
	"context"
	"net/url"
	"time"
)

type OAuthTokenRotatorOptions struct {
	Vault *OAuthTokenVault
	Flow  *OAuthDeviceFlow
}

type OAuthTokenRotationRequest struct {
	CredentialRef OAuthTokenCredentialRef
	TokenURL      string
	ClientID      string
	Scope         string
	RequestID     string
	Timeout       time.Duration
	ExtraForm     url.Values
}

type OAuthTokenRotationResult struct {
	CredentialRef       OAuthTokenCredentialRef
	TokenType           string
	Scope               string
	ExpiresIn           time.Duration
	RefreshTokenRotated bool
}

func (result OAuthTokenRotationResult) String() string {
	if result.CredentialRef.Key == "" {
		return "oauth-token-rotation:<invalid>"
	}
	return "oauth-token-rotation:" + result.CredentialRef.Key
}

type OAuthTokenRotator struct {
	vault *OAuthTokenVault
	flow  *OAuthDeviceFlow
}

func NewOAuthTokenRotator(options OAuthTokenRotatorOptions) (*OAuthTokenRotator, error) {
	if options.Vault == nil || options.Flow == nil {
		return nil, ErrInvalidOAuthConfig
	}
	return &OAuthTokenRotator{vault: options.Vault, flow: options.Flow}, nil
}

func (rotator *OAuthTokenRotator) Rotate(ctx context.Context, request OAuthTokenRotationRequest) (OAuthTokenRotationResult, error) {
	if rotator == nil || rotator.vault == nil || rotator.flow == nil || ctx == nil {
		return OAuthTokenRotationResult{}, ErrInvalidOAuthConfig
	}
	current, err := rotator.vault.Load(ctx, request.CredentialRef)
	if err != nil {
		return OAuthTokenRotationResult{}, err
	}
	if current.RefreshToken == "" {
		return OAuthTokenRotationResult{}, ErrOAuthRefreshUnavailable
	}
	next, err := rotator.flow.RefreshToken(ctx, OAuthRefreshTokenRequest{
		TokenURL: request.TokenURL, ClientID: request.ClientID, RefreshToken: current.RefreshToken,
		Scope: request.Scope, RequestID: request.RequestID, Timeout: request.Timeout, ExtraForm: request.ExtraForm,
	})
	if err != nil {
		return OAuthTokenRotationResult{}, err
	}
	refreshTokenRotated := next.RefreshToken != "" && next.RefreshToken != current.RefreshToken
	if next.RefreshToken == "" {
		next.RefreshToken = current.RefreshToken
	}
	ref, err := rotator.vault.Save(ctx, request.CredentialRef, next)
	if err != nil {
		return OAuthTokenRotationResult{}, err
	}
	return OAuthTokenRotationResult{
		CredentialRef: ref, TokenType: next.TokenType, Scope: next.Scope, ExpiresIn: next.ExpiresIn,
		RefreshTokenRotated: refreshTokenRotated,
	}, nil
}
