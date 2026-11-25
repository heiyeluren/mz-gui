//go:build windows

package remotedebug

import (
	"os/exec"
	"strconv"
)

func TryOpenFirewall(port int) {
	name := "mzgui-remote-debug"
	_ = exec.Command("netsh", "advfirewall", "firewall", "delete", "rule", "name="+name).Run()
	_ = exec.Command("netsh", "advfirewall", "firewall", "add", "rule",
		"name="+name, "dir=in", "action=allow", "protocol=TCP", "localport="+strconv.Itoa(port)).Run()
}
