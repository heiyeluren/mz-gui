//go:build !darwin && !windows && !linux

package local

import (
	"fmt"
	"net"
)

func CurrentUserPeerIdentity() (PeerIdentity, error) {
	return PeerIdentity{}, fmt.Errorf("当前平台不支持本机 peer 身份")
}

func PeerIdentityFromConn(net.Conn) (PeerIdentity, error) {
	return PeerIdentity{}, fmt.Errorf("当前平台不支持本机 peer 身份")
}
