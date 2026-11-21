//go:build darwin

package httpclient

import (
	"net"
	"strings"
	"syscall"

	"golang.org/x/sys/unix"
)

const interfaceBindingSupported = true

func bindSocket(raw syscall.RawConn, network string, iface *net.Interface) error {
	var bindErr error
	err := raw.Control(func(fd uintptr) {
		if strings.HasSuffix(network, "6") {
			bindErr = unix.SetsockoptInt(int(fd), unix.IPPROTO_IPV6, unix.IPV6_BOUND_IF, iface.Index)
		} else {
			bindErr = unix.SetsockoptInt(int(fd), unix.IPPROTO_IP, unix.IP_BOUND_IF, iface.Index)
		}
	})
	if err != nil {
		return err
	}
	return bindErr
}
