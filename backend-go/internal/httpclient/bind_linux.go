//go:build linux

package httpclient

import (
	"net"
	"syscall"

	"golang.org/x/sys/unix"
)

const interfaceBindingSupported = true

func bindSocket(raw syscall.RawConn, _ string, iface *net.Interface) error {
	var bindErr error
	err := raw.Control(func(fd uintptr) {
		bindErr = unix.SetsockoptString(int(fd), unix.SOL_SOCKET, unix.SO_BINDTODEVICE, iface.Name)
	})
	if err != nil {
		return err
	}
	return bindErr
}
