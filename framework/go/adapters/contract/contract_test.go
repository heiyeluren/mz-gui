package contract

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/heiyeluren/mz-gui/framework/go/core/health"
	"github.com/heiyeluren/mz-gui/framework/go/core/model"
)

func TestAdapterInfoValidateRejectsInvalidContracts(t *testing.T) {
	version := mustVersion(t, "1.0.0")
	tests := []struct {
		name string
		info AdapterInfo
	}{
		{name: "非法 ID", info: AdapterInfo{ID: "Bad ID", DisplayName: "Bad", BuildID: "build-1", Protocol: version, Capabilities: []Capability{{ID: "adapter.browser", Stability: StabilityExperimental}}}},
		{name: "缺少 capability", info: AdapterInfo{ID: "browser.bridge", DisplayName: "Browser Bridge", BuildID: "build-1", Protocol: version}},
		{name: "重复 capability", info: AdapterInfo{ID: "browser.bridge", DisplayName: "Browser Bridge", BuildID: "build-1", Protocol: version, Capabilities: []Capability{{ID: "adapter.browser", Stability: StabilityExperimental}, {ID: "adapter.browser", Stability: StabilityExperimental}}}},
		{name: "非法稳定性", info: AdapterInfo{ID: "browser.bridge", DisplayName: "Browser Bridge", BuildID: "build-1", Protocol: version, Capabilities: []Capability{{ID: "adapter.browser", Stability: "preview"}}}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if err := test.info.Validate(); !errors.Is(err, ErrInvalidContract) {
				t.Fatalf("应拒绝非法 adapter contract: %v", err)
			}
		})
	}
}

func TestAdapterInfoValidateAcceptsStableContract(t *testing.T) {
	info := AdapterInfo{
		ID:          "browser.bridge",
		DisplayName: "Browser Bridge",
		BuildID:     "build-20260816",
		Protocol:    mustVersion(t, "1.0.0"),
		Capabilities: []Capability{
			{ID: "adapter.browser", Stability: StabilityCandidate},
			{ID: "transport.remote-service", Stability: StabilityExperimental},
		},
		Limitations: []Limitation{{Code: "browser.edge-only", MessageKey: "adapter.browser.edge_only"}},
	}
	if err := info.Validate(); err != nil {
		t.Fatalf("有效 adapter contract 不应失败: %v", err)
	}
}

func TestStableInterfacesAreImplementableWithoutAppPackages(t *testing.T) {
	var _ BrowserBridge = fakeBrowserBridge{}
	var _ MediaProvider = fakeMediaProvider{}
	var _ MessageBus = fakeMessageBus{}
	var _ DriverAdapter = fakeDriverAdapter{}
	var _ RemoteService = fakeRemoteService{}
}

type fakeAdapter struct{}

func (fakeAdapter) Info(context.Context) (AdapterInfo, error)     { return AdapterInfo{}, nil }
func (fakeAdapter) Health(context.Context) (health.Report, error) { return health.Report{}, nil }
func (fakeAdapter) Close(context.Context) error                   { return nil }

type fakeBrowserBridge struct{ fakeAdapter }

func (fakeBrowserBridge) Pair(context.Context, PairRequest) (PairSession, error) {
	return PairSession{}, nil
}
func (fakeBrowserBridge) AuthorizeSite(context.Context, SiteAuthorizationRequest) (SiteGrant, error) {
	return SiteGrant{}, nil
}
func (fakeBrowserBridge) Open(context.Context, BrowserOpenRequest) (BrowserContext, error) {
	return BrowserContext{}, nil
}
func (fakeBrowserBridge) Execute(context.Context, BrowserAction) (TaskRef, error) {
	return TaskRef{}, nil
}
func (fakeBrowserBridge) Observe(context.Context, BrowserObserveRequest) (BrowserEventStream, error) {
	return nil, nil
}
func (fakeBrowserBridge) Revoke(context.Context, GrantID) error { return nil }

type fakeMediaProvider struct{ fakeAdapter }

func (fakeMediaProvider) Inspect(context.Context, MediaSource) (MediaInfo, error) {
	return MediaInfo{}, nil
}
func (fakeMediaProvider) Thumbnail(context.Context, ThumbnailRequest) (ArtifactRef, error) {
	return ArtifactRef{}, nil
}
func (fakeMediaProvider) OpenPlayback(context.Context, PlaybackRequest) (PlaybackSession, error) {
	return PlaybackSession{}, nil
}
func (fakeMediaProvider) Control(context.Context, PlaybackCommand) (PlaybackState, error) {
	return PlaybackState{}, nil
}
func (fakeMediaProvider) ClosePlayback(context.Context, PlaybackSessionID) error { return nil }

type fakeMessageBus struct{ fakeAdapter }

func (fakeMessageBus) Publish(context.Context, Message) (PublishAck, error) { return PublishAck{}, nil }
func (fakeMessageBus) Subscribe(context.Context, SubscriptionRequest) (MessageStream, error) {
	return nil, nil
}
func (fakeMessageBus) Ack(context.Context, DeliveryID) error   { return nil }
func (fakeMessageBus) Nack(context.Context, NackRequest) error { return nil }

type fakeDriverAdapter struct{ fakeAdapter }

func (fakeDriverAdapter) Probe(context.Context, ProbeRequest) (DeviceSet, error) {
	return DeviceSet{}, nil
}
func (fakeDriverAdapter) Authorize(context.Context, DeviceAuthorizationRequest) (DeviceGrant, error) {
	return DeviceGrant{}, nil
}
func (fakeDriverAdapter) Invoke(context.Context, DriverOperation) (TaskRef, error) {
	return TaskRef{}, nil
}
func (fakeDriverAdapter) Observe(context.Context, DeviceObserveRequest) (DeviceEventStream, error) {
	return nil, nil
}

type fakeRemoteService struct{ fakeAdapter }

func (fakeRemoteService) Authenticate(context.Context, AuthRequest) (AuthState, error) {
	return AuthState{}, nil
}
func (fakeRemoteService) Do(context.Context, RemoteRequest) (RemoteResponse, error) {
	return RemoteResponse{}, nil
}
func (fakeRemoteService) Stream(context.Context, RemoteStreamRequest) (RemoteEventStream, error) {
	return nil, nil
}
func (fakeRemoteService) Revoke(context.Context, AuthRef) error { return nil }

func mustVersion(t *testing.T, value string) model.Version {
	t.Helper()
	version, err := model.ParseVersion(value)
	if err != nil {
		t.Fatal(err)
	}
	return version
}

var futureMeta = RequestMeta{
	RequestID: model.ID("request-1"),
	TraceID:   model.ID("trace-1"),
	Deadline:  model.Timestamp(time.Now().UTC().Add(time.Hour).Format(time.RFC3339Nano)),
	Caller: CallerRef{
		Component:  "adapter-test",
		Version:    mustStaticVersion(),
		InstanceID: model.ID("instance-1"),
	},
}

func mustStaticVersion() model.Version {
	version, _ := model.ParseVersion("1.0.0")
	return version
}
