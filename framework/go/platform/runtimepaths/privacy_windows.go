//go:build windows

package runtimepaths

import (
	"os"
	"runtime"
	"syscall"
	"unsafe"

	"golang.org/x/sys/windows"
)

const windowsReparsePoint = uint32(syscall.FILE_ATTRIBUTE_REPARSE_POINT)

func isWindowsReparsePoint(info os.FileInfo) bool {
	attrs, ok := windowsFileAttributes(info)
	return ok && attrs&windowsReparsePoint != 0
}

func windowsFileAttributes(info os.FileInfo) (uint32, bool) {
	switch sys := info.Sys().(type) {
	case *syscall.Win32FileAttributeData:
		return sys.FileAttributes, true
	case syscall.Win32FileAttributeData:
		return sys.FileAttributes, true
	default:
		return 0, false
	}
}

func lockdownPrivateDir(path string) error {
	return restrictWindowsOwnerOnly(path, true)
}

func windowsOwnerOnly(path string) (ownerOnly bool, queryable bool) {
	sd, err := windows.GetNamedSecurityInfo(path, windows.SE_FILE_OBJECT, windows.OWNER_SECURITY_INFORMATION|windows.DACL_SECURITY_INFORMATION)
	if err != nil || sd == nil {
		return false, false
	}
	owner, _, err := sd.Owner()
	if err != nil || owner == nil {
		return false, false
	}
	dacl, _, err := sd.DACL()
	if err != nil {
		return false, false
	}
	if dacl == nil {
		return false, true
	}
	token := windows.GetCurrentProcessToken()
	user, err := token.GetTokenUser()
	if err != nil {
		return false, false
	}
	if !owner.Equals(user.User.Sid) && !owner.IsWellKnown(windows.WinLocalSystemSid) && !owner.IsWellKnown(windows.WinBuiltinAdministratorsSid) {
		return false, true
	}
	for i := uint32(0); i < uint32(dacl.AceCount); i++ {
		var ace *windows.ACCESS_ALLOWED_ACE
		if err := windows.GetAce(dacl, i, &ace); err != nil || ace == nil {
			return false, false
		}
		if ace.Header.AceType != windows.ACCESS_ALLOWED_ACE_TYPE {
			continue
		}
		sid := (*windows.SID)(unsafe.Pointer(&ace.SidStart))
		if sid.Equals(user.User.Sid) || sid.Equals(owner) || sid.IsWellKnown(windows.WinLocalSystemSid) || sid.IsWellKnown(windows.WinBuiltinAdministratorsSid) {
			continue
		}
		return false, true
	}
	return true, true
}

func privateRegularFile(path string, info os.FileInfo) bool {
	if isWindowsReparsePoint(info) {
		return false
	}
	ownerOnly, queryable := windowsOwnerOnly(path)
	return queryable && ownerOnly
}

func restrictWindowsOwnerOnly(path string, directory bool) error {
	token := windows.GetCurrentProcessToken()
	user, err := token.GetTokenUser()
	if err != nil {
		return err
	}
	systemSid, err := windows.CreateWellKnownSid(windows.WinLocalSystemSid)
	if err != nil {
		return err
	}
	adminSid, err := windows.CreateWellKnownSid(windows.WinBuiltinAdministratorsSid)
	if err != nil {
		return err
	}
	var pinner runtime.Pinner
	defer pinner.Unpin()
	pinner.Pin(user.User.Sid)
	pinner.Pin(systemSid)
	pinner.Pin(adminSid)
	inheritance := uint32(0)
	if directory {
		inheritance = windows.SUB_CONTAINERS_AND_OBJECTS_INHERIT
	}
	access := []windows.EXPLICIT_ACCESS{
		explicitAllow(user.User.Sid, windows.TRUSTEE_IS_USER, inheritance),
		explicitAllow(systemSid, windows.TRUSTEE_IS_GROUP, inheritance),
		explicitAllow(adminSid, windows.TRUSTEE_IS_GROUP, inheritance),
	}
	acl, err := windows.ACLFromEntries(access, nil)
	if err != nil {
		return err
	}
	return windows.SetNamedSecurityInfo(path, windows.SE_FILE_OBJECT, windows.DACL_SECURITY_INFORMATION|windows.PROTECTED_DACL_SECURITY_INFORMATION, nil, nil, acl, nil)
}

func explicitAllow(sid *windows.SID, trusteeType windows.TRUSTEE_TYPE, inheritance uint32) windows.EXPLICIT_ACCESS {
	return windows.EXPLICIT_ACCESS{
		AccessPermissions: windows.GENERIC_ALL,
		AccessMode:        windows.GRANT_ACCESS,
		Inheritance:       inheritance,
		Trustee: windows.TRUSTEE{
			TrusteeForm:  windows.TRUSTEE_IS_SID,
			TrusteeType:  trusteeType,
			TrusteeValue: windows.TrusteeValueFromSID(sid),
		},
	}
}
