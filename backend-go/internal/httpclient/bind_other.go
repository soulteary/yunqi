//go:build !linux && !darwin

package httpclient

import (
	"errors"
	"net"
	"syscall"
)

const interfaceBindingSupported = false

func bindSocket(syscall.RawConn, string, *net.Interface) error {
	return errors.New("interface binding unsupported")
}
