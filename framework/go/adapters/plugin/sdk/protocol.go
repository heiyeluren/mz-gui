// Package sdk 提供 Framework 插件进程协议的最小 SDK。
package sdk

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
)

const (
	FrameHandshake       = "plugin.handshake.v1"
	FrameCapabilityCheck = "plugin.capability.check.v1"
	FrameShutdown        = "plugin.shutdown.v1"
)

var ErrProtocol = errors.New("Framework 插件 SDK 协议错误")

type Descriptor struct {
	PluginID      string   `json:"plugin_id"`
	ProtocolMajor uint32   `json:"protocol_major"`
	ProtocolMinor uint32   `json:"protocol_minor"`
	BuildID       string   `json:"build_id"`
	Capabilities  []string `json:"capabilities"`
}

type HandshakeRequest struct {
	ProtocolMajor         uint32   `json:"protocol_major"`
	HostID                string   `json:"host_id"`
	RequestedCapabilities []string `json:"requested_capabilities"`
}

type HandshakeResponse struct {
	PluginID            string   `json:"plugin_id"`
	ProtocolMajor       uint32   `json:"protocol_major"`
	ProtocolMinor       uint32   `json:"protocol_minor"`
	BuildID             string   `json:"build_id"`
	GrantedCapabilities []string `json:"granted_capabilities"`
	DegradedReasons     []string `json:"degraded_reasons"`
}

type CapabilityRequest struct {
	Capability string `json:"capability"`
}

type CapabilityResponse struct {
	Allowed bool   `json:"allowed"`
	Reason  string `json:"reason,omitempty"`
}

type ShutdownRequest struct {
	Reason string `json:"reason"`
}

type ShutdownResponse struct {
	Accepted bool `json:"accepted"`
}

type Handler interface {
	Handshake(context.Context, HandshakeRequest) (HandshakeResponse, error)
	CheckCapability(context.Context, CapabilityRequest) (CapabilityResponse, error)
	Shutdown(context.Context, ShutdownRequest) (ShutdownResponse, error)
}

type StaticHandler struct {
	Descriptor Descriptor
}

func (handler StaticHandler) Handshake(_ context.Context, request HandshakeRequest) (HandshakeResponse, error) {
	if request.ProtocolMajor != handler.Descriptor.ProtocolMajor || request.HostID == "" {
		return HandshakeResponse{}, fmt.Errorf("%w: handshake rejected", ErrProtocol)
	}
	allowed := capabilitySet(handler.Descriptor.Capabilities)
	granted := make([]string, 0, len(request.RequestedCapabilities))
	for _, capability := range request.RequestedCapabilities {
		if _, ok := allowed[capability]; ok {
			granted = append(granted, capability)
		}
	}
	sort.Strings(granted)
	return HandshakeResponse{
		PluginID:            handler.Descriptor.PluginID,
		ProtocolMajor:       handler.Descriptor.ProtocolMajor,
		ProtocolMinor:       handler.Descriptor.ProtocolMinor,
		BuildID:             handler.Descriptor.BuildID,
		GrantedCapabilities: granted,
	}, nil
}

func (handler StaticHandler) CheckCapability(_ context.Context, request CapabilityRequest) (CapabilityResponse, error) {
	if strings.TrimSpace(request.Capability) == "" {
		return CapabilityResponse{}, fmt.Errorf("%w: empty capability", ErrProtocol)
	}
	if _, ok := capabilitySet(handler.Descriptor.Capabilities)[request.Capability]; ok {
		return CapabilityResponse{Allowed: true}, nil
	}
	return CapabilityResponse{Allowed: false, Reason: "unsupported"}, nil
}

func (handler StaticHandler) Shutdown(context.Context, ShutdownRequest) (ShutdownResponse, error) {
	return ShutdownResponse{Accepted: true}, nil
}

type requestFrame struct {
	ID      string          `json:"id"`
	Type    string          `json:"type"`
	Payload json.RawMessage `json:"payload"`
}

type responseFrame struct {
	ID      string          `json:"id"`
	Type    string          `json:"type"`
	Payload json.RawMessage `json:"payload,omitempty"`
	Error   *frameError     `json:"error,omitempty"`
}

type frameError struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

func ServeJSONLines(ctx context.Context, reader io.Reader, writer io.Writer, handler Handler) error {
	decoder := json.NewDecoder(reader)
	encoder := json.NewEncoder(writer)
	for {
		if err := ctx.Err(); err != nil {
			return err
		}
		var frame requestFrame
		if err := decoder.Decode(&frame); err != nil {
			if errors.Is(err, io.EOF) {
				return nil
			}
			return fmt.Errorf("%w: decode request: %v", ErrProtocol, err)
		}
		shouldStop, err := serveFrame(ctx, encoder, handler, frame)
		if err != nil {
			return err
		}
		if shouldStop {
			return nil
		}
	}
}

func serveFrame(ctx context.Context, encoder *json.Encoder, handler Handler, frame requestFrame) (bool, error) {
	if frame.ID == "" || frame.Type == "" {
		return false, fmt.Errorf("%w: missing frame identity", ErrProtocol)
	}
	var payload any
	var handlerErr error
	shouldStop := false
	switch frame.Type {
	case FrameHandshake:
		var request HandshakeRequest
		if err := decodePayload(frame.Payload, &request); err != nil {
			handlerErr = err
			break
		}
		payload, handlerErr = handler.Handshake(ctx, request)
	case FrameCapabilityCheck:
		var request CapabilityRequest
		if err := decodePayload(frame.Payload, &request); err != nil {
			handlerErr = err
			break
		}
		payload, handlerErr = handler.CheckCapability(ctx, request)
	case FrameShutdown:
		var request ShutdownRequest
		if err := decodePayload(frame.Payload, &request); err != nil {
			handlerErr = err
			break
		}
		payload, handlerErr = handler.Shutdown(ctx, request)
		shouldStop = handlerErr == nil
	default:
		handlerErr = fmt.Errorf("%w: unknown frame type %s", ErrProtocol, frame.Type)
	}
	response := responseFrame{ID: frame.ID, Type: frame.Type}
	if handlerErr != nil {
		response.Error = &frameError{Code: "protocol_error", Message: handlerErr.Error()}
	} else {
		data, err := json.Marshal(payload)
		if err != nil {
			return false, fmt.Errorf("%w: encode response payload: %v", ErrProtocol, err)
		}
		response.Payload = data
	}
	if err := encoder.Encode(response); err != nil {
		return false, fmt.Errorf("%w: encode response: %v", ErrProtocol, err)
	}
	return shouldStop, nil
}

func decodePayload(data json.RawMessage, target any) error {
	if len(data) == 0 {
		return fmt.Errorf("%w: missing payload", ErrProtocol)
	}
	if err := json.Unmarshal(data, target); err != nil {
		return fmt.Errorf("%w: decode payload: %v", ErrProtocol, err)
	}
	return nil
}

type Client struct {
	decoder *json.Decoder
	encoder *json.Encoder
	nextID  atomic.Uint64
	mu      sync.Mutex
}

func NewClient(reader io.Reader, writer io.Writer) *Client {
	return &Client{
		decoder: json.NewDecoder(reader),
		encoder: json.NewEncoder(writer),
	}
}

func (client *Client) Handshake(ctx context.Context, request HandshakeRequest) (HandshakeResponse, error) {
	var response HandshakeResponse
	if err := client.call(ctx, FrameHandshake, request, &response); err != nil {
		return HandshakeResponse{}, err
	}
	return response, nil
}

func (client *Client) CheckCapability(ctx context.Context, request CapabilityRequest) (CapabilityResponse, error) {
	var response CapabilityResponse
	if err := client.call(ctx, FrameCapabilityCheck, request, &response); err != nil {
		return CapabilityResponse{}, err
	}
	return response, nil
}

func (client *Client) Shutdown(ctx context.Context, request ShutdownRequest) error {
	var response ShutdownResponse
	if err := client.call(ctx, FrameShutdown, request, &response); err != nil {
		return err
	}
	if !response.Accepted {
		return fmt.Errorf("%w: shutdown rejected", ErrProtocol)
	}
	return nil
}

func (client *Client) call(ctx context.Context, frameType string, request any, response any) error {
	client.mu.Lock()
	defer client.mu.Unlock()
	id := fmt.Sprintf("sdk-%d", client.nextID.Add(1))
	payload, err := json.Marshal(request)
	if err != nil {
		return fmt.Errorf("%w: encode request payload: %v", ErrProtocol, err)
	}
	if err := client.encoder.Encode(requestFrame{ID: id, Type: frameType, Payload: payload}); err != nil {
		return fmt.Errorf("%w: encode request: %v", ErrProtocol, err)
	}
	result := make(chan responseFrame, 1)
	failures := make(chan error, 1)
	go func() {
		var frame responseFrame
		if err := client.decoder.Decode(&frame); err != nil {
			failures <- err
			return
		}
		result <- frame
	}()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case err := <-failures:
		return fmt.Errorf("%w: decode response: %v", ErrProtocol, err)
	case frame := <-result:
		if frame.ID != id || frame.Type != frameType {
			return fmt.Errorf("%w: mismatched response", ErrProtocol)
		}
		if frame.Error != nil {
			return fmt.Errorf("%w: %s", ErrProtocol, frame.Error.Message)
		}
		if err := json.Unmarshal(frame.Payload, response); err != nil {
			return fmt.Errorf("%w: decode response payload: %v", ErrProtocol, err)
		}
		return nil
	}
}

func capabilitySet(capabilities []string) map[string]struct{} {
	result := make(map[string]struct{}, len(capabilities))
	for _, capability := range capabilities {
		result[capability] = struct{}{}
	}
	return result
}
