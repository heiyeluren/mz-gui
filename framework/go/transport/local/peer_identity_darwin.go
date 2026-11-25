//go:build darwin

package local

import (
	"fmt"
	"net"
	"os/user"
	"strconv"
	"syscall"

	"golang.org/x/sys/unix"
)

type syscallConn interface {
	SyscallConn() (syscall.RawConn, error)
}

func CurrentUserPeerIdentity() (PeerIdentity, error) {
	current, err := user.Current()
	if err != nil {
		return PeerIdentity{}, fmt.Errorf("读取当前用户 UID: %w", err)
	}
	return PeerIdentity{Kind: PeerIdentityKindUID, Value: current.Uid}, nil
}

func PeerIdentityFromConn(connection net.Conn) (PeerIdentity, error) {
	provider, ok := connection.(syscallConn)
	if !ok {
		return PeerIdentity{}, fmt.Errorf("连接不支持读取文件描述符")
	}
	raw, err := provider.SyscallConn()
	if err != nil {
		return PeerIdentity{}, fmt.Errorf("读取连接文件描述符: %w", err)
	}
	var credential *unix.Xucred
	var credentialErr error
	if err := raw.Control(func(fd uintptr) {
		credential, credentialErr = unix.GetsockoptXucred(int(fd), unix.SOL_LOCAL, unix.LOCAL_PEERCRED)
	}); err != nil {
		return PeerIdentity{}, fmt.Errorf("读取 UDS peer 凭据: %w", err)
	}
	if credentialErr != nil {
		return PeerIdentity{}, fmt.Errorf("读取 UDS peer 凭据: %w", credentialErr)
	}
	return PeerIdentity{Kind: PeerIdentityKindUID, Value: strconv.FormatUint(uint64(credential.Uid), 10)}, nil
}
