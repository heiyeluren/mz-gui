// Package contract 定义 Framework 扩展层的稳定接口和产品无关 DTO。
package contract

import (
	"context"
	"errors"
	"regexp"
	"strings"

	"github.com/heiyeluren/mz-gui/framework/go/core/health"
	"github.com/heiyeluren/mz-gui/framework/go/core/model"
)

var ErrInvalidContract = errors.New("Framework Adapter 契约无效")

var (
	adapterIDPattern  = regexp.MustCompile(`^[a-z][a-z0-9]*(?:\.[a-z][a-z0-9-]*)+$`)
	buildIDPattern    = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._:-]{0,127}$`)
	messageKeyPattern = regexp.MustCompile(`^[a-z][a-z0-9_]*(?:\.[a-z][a-z0-9_]*)+$`)
)

type Stability string

const (
	StabilityExperimental Stability = "experimental"
	StabilityCandidate    Stability = "candidate"
	StabilityStable       Stability = "stable"
	StabilityDeprecated   Stability = "deprecated"
)

type Capability struct {
	ID        string    `json:"id"`
	Stability Stability `json:"stability"`
}

type Limitation struct {
	Code       string `json:"code"`
	MessageKey string `json:"message_key"`
}

type AdapterInfo struct {
	ID           string        `json:"id"`
	DisplayName  string        `json:"display_name"`
	BuildID      string        `json:"build_id"`
	Protocol     model.Version `json:"protocol"`
	Capabilities []Capability  `json:"capabilities"`
	Limitations  []Limitation  `json:"limitations,omitempty"`
}

func (info AdapterInfo) Validate() error {
	if !adapterIDPattern.MatchString(info.ID) || strings.TrimSpace(info.DisplayName) == "" ||
		len(info.DisplayName) > 80 || !buildIDPattern.MatchString(info.BuildID) ||
		info.Protocol.Major == 0 || len(info.Capabilities) == 0 {
		return ErrInvalidContract
	}
	seenCapabilities := make(map[string]struct{}, len(info.Capabilities))
	for _, capability := range info.Capabilities {
		if !adapterIDPattern.MatchString(capability.ID) || !capability.Stability.Valid() {
			return ErrInvalidContract
		}
		if _, exists := seenCapabilities[capability.ID]; exists {
			return ErrInvalidContract
		}
		seenCapabilities[capability.ID] = struct{}{}
	}
	seenLimitations := make(map[string]struct{}, len(info.Limitations))
	for _, limitation := range info.Limitations {
		if !adapterIDPattern.MatchString(limitation.Code) || !messageKeyPattern.MatchString(limitation.MessageKey) {
			return ErrInvalidContract
		}
		if _, exists := seenLimitations[limitation.Code]; exists {
			return ErrInvalidContract
		}
		seenLimitations[limitation.Code] = struct{}{}
	}
	return nil
}

func (stability Stability) Valid() bool {
	switch stability {
	case StabilityExperimental, StabilityCandidate, StabilityStable, StabilityDeprecated:
		return true
	default:
		return false
	}
}

type CallerRef = model.Caller
type RequestMeta = model.RequestMeta
type GrantID = model.ID
type DeliveryID = model.ID
type PlaybackSessionID = model.ID
type AuthRef = model.ID

type TaskRef struct {
	ID   model.ID `json:"id"`
	Type string   `json:"type"`
}

type ArtifactRef struct {
	ID     model.ID `json:"id"`
	SHA256 string   `json:"sha256"`
	Bytes  int64    `json:"bytes"`
}

type Adapter interface {
	Info(context.Context) (AdapterInfo, error)
	Health(context.Context) (health.Report, error)
	Close(context.Context) error
}

type BrowserBridge interface {
	Adapter
	Pair(context.Context, PairRequest) (PairSession, error)
	AuthorizeSite(context.Context, SiteAuthorizationRequest) (SiteGrant, error)
	Open(context.Context, BrowserOpenRequest) (BrowserContext, error)
	Execute(context.Context, BrowserAction) (TaskRef, error)
	Observe(context.Context, BrowserObserveRequest) (BrowserEventStream, error)
	Revoke(context.Context, GrantID) error
}

type MediaProvider interface {
	Adapter
	Inspect(context.Context, MediaSource) (MediaInfo, error)
	Thumbnail(context.Context, ThumbnailRequest) (ArtifactRef, error)
	OpenPlayback(context.Context, PlaybackRequest) (PlaybackSession, error)
	Control(context.Context, PlaybackCommand) (PlaybackState, error)
	ClosePlayback(context.Context, PlaybackSessionID) error
}

type MessageBus interface {
	Adapter
	Publish(context.Context, Message) (PublishAck, error)
	Subscribe(context.Context, SubscriptionRequest) (MessageStream, error)
	Ack(context.Context, DeliveryID) error
	Nack(context.Context, NackRequest) error
}

type DriverAdapter interface {
	Adapter
	Probe(context.Context, ProbeRequest) (DeviceSet, error)
	Authorize(context.Context, DeviceAuthorizationRequest) (DeviceGrant, error)
	Invoke(context.Context, DriverOperation) (TaskRef, error)
	Observe(context.Context, DeviceObserveRequest) (DeviceEventStream, error)
}

type RemoteService interface {
	Adapter
	Authenticate(context.Context, AuthRequest) (AuthState, error)
	Do(context.Context, RemoteRequest) (RemoteResponse, error)
	Stream(context.Context, RemoteStreamRequest) (RemoteEventStream, error)
	Revoke(context.Context, AuthRef) error
}
