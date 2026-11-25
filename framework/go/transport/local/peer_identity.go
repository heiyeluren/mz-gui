package local

import (
	"fmt"
	"net"
	"strings"
)

const (
	PeerIdentityKindUID = "uid"
	PeerIdentityKindSID = "sid"
)

// PeerIdentity 描述本机 IPC 对端的操作系统身份。
type PeerIdentity struct {
	Kind  string
	Value string
}

func (identity PeerIdentity) Validate() error {
	if identity.Kind == "" || identity.Value == "" {
		return fmt.Errorf("peer 身份类型和值不能为空")
	}
	switch identity.Kind {
	case PeerIdentityKindUID, PeerIdentityKindSID:
		return nil
	default:
		return fmt.Errorf("不支持的 peer 身份类型: %s", identity.Kind)
	}
}

func (identity PeerIdentity) Matches(other PeerIdentity) bool {
	if identity.Kind != other.Kind {
		return false
	}
	if identity.Kind == PeerIdentityKindSID {
		return strings.EqualFold(identity.Value, other.Value)
	}
	return identity.Value == other.Value
}

type PeerPolicy struct {
	Required bool
	Allowed  PeerIdentity
}

func RequirePeerIdentity(identity PeerIdentity) PeerPolicy {
	return PeerPolicy{Required: true, Allowed: identity}
}

func (policy PeerPolicy) Validate() error {
	if !policy.Required {
		return nil
	}
	if err := policy.Allowed.Validate(); err != nil {
		return fmt.Errorf("peer 身份策略无效: %w", err)
	}
	return nil
}

func (policy PeerPolicy) Verify(identity PeerIdentity) error {
	if !policy.Required {
		return nil
	}
	if err := identity.Validate(); err != nil {
		return fmt.Errorf("peer 身份无效: %w", err)
	}
	if !policy.Allowed.Matches(identity) {
		return fmt.Errorf("peer 身份不匹配: got %s:%s", identity.Kind, identity.Value)
	}
	return nil
}

type peerVerifyingListener struct {
	net.Listener
	policy PeerPolicy
}

func (listener *peerVerifyingListener) Accept() (net.Conn, error) {
	for {
		connection, err := listener.Listener.Accept()
		if err != nil {
			return nil, err
		}
		identity, err := PeerIdentityFromConn(connection)
		if err == nil {
			err = listener.policy.Verify(identity)
		}
		if err == nil {
			return connection, nil
		}
		_ = connection.Close()
	}
}
