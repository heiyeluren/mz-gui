//go:build windows

package local

import (
	"fmt"
	"net"
	"os/user"
	"syscall"

	"golang.org/x/sys/windows"
)

var procImpersonateNamedPipeClient = windows.NewLazySystemDLL("advapi32.dll").NewProc("ImpersonateNamedPipeClient")

type fdConn interface {
	Fd() uintptr
}

func CurrentUserPeerIdentity() (PeerIdentity, error) {
	current, err := user.Current()
	if err != nil {
		return PeerIdentity{}, fmt.Errorf("读取当前 Windows 用户 SID: %w", err)
	}
	return PeerIdentity{Kind: PeerIdentityKindSID, Value: current.Uid}, nil
}

func PeerIdentityFromConn(connection net.Conn) (PeerIdentity, error) {
	provider, ok := connection.(fdConn)
	if !ok {
		return PeerIdentity{}, fmt.Errorf("连接不支持读取 Windows handle")
	}
	if err := impersonateNamedPipeClient(windows.Handle(provider.Fd())); err != nil {
		return PeerIdentity{}, fmt.Errorf("模拟 Named Pipe 客户端身份: %w", err)
	}
	defer windows.RevertToSelf()

	var token windows.Token
	if err := windows.OpenThreadToken(windows.CurrentThread(), windows.TOKEN_QUERY, false, &token); err != nil {
		return PeerIdentity{}, fmt.Errorf("读取 Named Pipe 客户端 token: %w", err)
	}
	defer token.Close()

	tokenUser, err := token.GetTokenUser()
	if err != nil {
		return PeerIdentity{}, fmt.Errorf("读取 Named Pipe 客户端 SID: %w", err)
	}
	sid := tokenUser.User.Sid.String()
	return PeerIdentity{Kind: PeerIdentityKindSID, Value: sid}, nil
}

func impersonateNamedPipeClient(handle windows.Handle) error {
	result, _, callErr := procImpersonateNamedPipeClient.Call(uintptr(handle))
	if result != 0 {
		return nil
	}
	if callErr != syscall.Errno(0) {
		return callErr
	}
	return syscall.EINVAL
}
