package admin

import (
	"net"
	"os"
	"syscall"
)

type uidListener struct{ net.Listener }

func (l *uidListener) Accept() (net.Conn, error) {
	for {
		c, err := l.Listener.Accept()
		if err != nil {
			return nil, err
		}
		u, ok := c.(*net.UnixConn)
		if !ok {
			c.Close()
			continue
		}
		raw, err := u.SyscallConn()
		if err != nil {
			c.Close()
			continue
		}
		var peer *syscall.Ucred
		var peerErr error
		if err = raw.Control(func(fd uintptr) {
			peer, peerErr = syscall.GetsockoptUcred(int(fd), syscall.SOL_SOCKET, syscall.SO_PEERCRED)
		}); err != nil || peerErr != nil || peer == nil || peer.Uid != uint32(os.Getuid()) {
			c.Close()
			continue
		}
		return c, nil
	}
}
