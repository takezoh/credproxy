//go:build windows

package nativeipc

import (
	"bytes"
	"fmt"
	"io"
	"net"
	"runtime"
	"time"

	"golang.org/x/sys/windows"
)

var impersonatePipeClient = windows.NewLazySystemDLL("advapi32.dll").NewProc("ImpersonateNamedPipeClient")

type sidListener struct {
	net.Listener
	expected string
}

type identifiedConn struct {
	net.Conn
	sid string
}

func (c *identifiedConn) PeerSID() string { return c.sid }

func (l *sidListener) Accept() (net.Conn, error) {
	for {
		conn, err := l.Listener.Accept()
		if err != nil {
			return nil, err
		}
		sid, err := authenticatePipeClient(conn, l.expected, pipeHandshakeTimeout)
		if err != nil || !SIDMatches(sid, l.expected) {
			conn.Close()
			continue
		}
		return &identifiedConn{Conn: conn, sid: sid}, nil
	}
}

const pipeHandshakeTimeout = 5 * time.Second

func authenticatePipeClient(conn net.Conn, expected string, timeout time.Duration) (string, error) {
	if err := conn.SetDeadline(time.Now().Add(timeout)); err != nil {
		return "", fmt.Errorf("set named pipe handshake deadline: %w", err)
	}
	preface := make([]byte, len(pipeClientPreface))
	if _, err := io.ReadFull(conn, preface); err != nil {
		return "", fmt.Errorf("read named pipe client preface: %w", err)
	}
	if !bytes.Equal(preface, pipeClientPreface) {
		return "", fmt.Errorf("invalid named pipe client preface")
	}
	if err := conn.SetDeadline(time.Time{}); err != nil {
		return "", fmt.Errorf("clear named pipe handshake deadline: %w", err)
	}
	sid, err := impersonatedSID(conn)
	if err != nil || !SIDMatches(sid, expected) {
		return "", fmt.Errorf("named pipe client SID verification failed")
	}
	if err := conn.SetDeadline(time.Now().Add(timeout)); err != nil {
		return "", fmt.Errorf("set named pipe acknowledgement deadline: %w", err)
	}
	if err := writeAll(conn, pipeServerAck); err != nil {
		return "", fmt.Errorf("write named pipe acknowledgement: %w", err)
	}
	if err := conn.SetDeadline(time.Time{}); err != nil {
		return "", fmt.Errorf("clear named pipe acknowledgement deadline: %w", err)
	}
	return sid, nil
}

func writeAll(conn net.Conn, data []byte) error {
	for len(data) > 0 {
		written, err := conn.Write(data)
		if err != nil {
			return err
		}
		if written == 0 {
			return fmt.Errorf("named pipe write made no progress")
		}
		data = data[written:]
	}
	return nil
}

func impersonatedSID(conn net.Conn) (string, error) {
	result := make(chan sidResult, 1)
	go func() {
		runtime.LockOSThread()
		sid, err, reverted := impersonatedSIDOnThread(conn)
		if !reverted {
			result <- sidResult{err: err}
			return
		}
		runtime.UnlockOSThread()
		result <- sidResult{sid: sid, err: err}
	}()
	value := <-result
	return value.sid, value.err
}

type sidResult struct {
	sid string
	err error
}

func impersonatedSIDOnThread(conn net.Conn) (sid string, result error, reverted bool) {
	pipe, ok := conn.(interface{ Fd() uintptr })
	if !ok {
		return "", fmt.Errorf("named pipe connection has no native handle"), true
	}
	if err := callImpersonatePipeClient(windows.Handle(pipe.Fd())); err != nil {
		return "", err, true
	}
	defer func() {
		if err := windows.RevertToSelf(); err != nil {
			result = fmt.Errorf("revert named pipe impersonation: %w", err)
			reverted = false
		}
	}()
	thread, err := windows.GetCurrentThread()
	if err != nil {
		return "", err, true
	}
	var token windows.Token
	if err := windows.OpenThreadToken(thread, windows.TOKEN_QUERY, true, &token); err != nil {
		return "", fmt.Errorf("open impersonated pipe token: %w", err), true
	}
	defer token.Close()
	user, err := token.GetTokenUser()
	if err != nil {
		return "", fmt.Errorf("read impersonated pipe SID: %w", err), true
	}
	return user.User.Sid.String(), nil, true
}

func callImpersonatePipeClient(pipe windows.Handle) error {
	result, _, callErr := impersonatePipeClient.Call(uintptr(pipe))
	if result != 0 {
		return nil
	}
	if callErr != windows.ERROR_SUCCESS {
		return callErr
	}
	return windows.ERROR_ACCESS_DENIED
}
