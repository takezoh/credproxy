//go:build windows

package nativeipc

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"net"
	"strings"
	"time"

	"github.com/Microsoft/go-winio"
	"github.com/takezoh/credproxy/internal/winsecurefile"
	"golang.org/x/sys/windows"
)

// DialPipe verifies the connected server process before returning a connection
// that an HTTP client can use to transmit credentials or requests.
func DialPipe(ctx context.Context, path, expectedExecutable string) (net.Conn, error) {
	if err := validatePipePath(path); err != nil {
		return nil, err
	}
	if strings.TrimSpace(expectedExecutable) == "" {
		return nil, fmt.Errorf("expected managed server executable is required")
	}
	// Identification lets the server inspect our SID without impersonating us.
	conn, err := winio.DialPipeAccessImpLevel(ctx, path, windows.GENERIC_READ|windows.GENERIC_WRITE, winio.PipeImpLevelIdentification)
	if err != nil {
		return nil, err
	}
	if err := verifyPipeServer(conn, expectedExecutable); err != nil {
		conn.Close()
		return nil, err
	}
	if err := clientPipeHandshake(ctx, conn); err != nil {
		conn.Close()
		return nil, err
	}
	return conn, nil
}

var pipeClientPreface = []byte("credproxy-native-v1\n")
var pipeServerAck = []byte("credproxy-ready-v1\n")

func clientPipeHandshake(ctx context.Context, conn net.Conn) error {
	deadline := time.Now().Add(pipeHandshakeTimeout)
	if contextDeadline, ok := ctx.Deadline(); ok && contextDeadline.Before(deadline) {
		deadline = contextDeadline
	}
	if err := conn.SetDeadline(deadline); err != nil {
		return fmt.Errorf("set named pipe handshake deadline: %w", err)
	}
	if err := writeAll(conn, pipeClientPreface); err != nil {
		return fmt.Errorf("write named pipe client preface: %w", err)
	}
	ack := make([]byte, len(pipeServerAck))
	if _, err := io.ReadFull(conn, ack); err != nil {
		return fmt.Errorf("read named pipe acknowledgement: %w", err)
	}
	if !bytes.Equal(ack, pipeServerAck) {
		return fmt.Errorf("invalid named pipe acknowledgement")
	}
	if err := conn.SetDeadline(time.Time{}); err != nil {
		return fmt.Errorf("clear named pipe handshake deadline: %w", err)
	}
	return nil
}

func verifyPipeServer(conn net.Conn, expectedExecutable string) error {
	pipe, ok := conn.(interface{ Fd() uintptr })
	if !ok {
		return fmt.Errorf("named pipe connection has no native handle")
	}
	var processID uint32
	if err := windows.GetNamedPipeServerProcessId(windows.Handle(pipe.Fd()), &processID); err != nil {
		return fmt.Errorf("read named pipe server process identity: %w", err)
	}
	if processID == 0 {
		return fmt.Errorf("named pipe server process identity is missing")
	}
	process, err := windows.OpenProcess(windows.PROCESS_QUERY_LIMITED_INFORMATION, false, processID)
	if err != nil {
		return fmt.Errorf("open named pipe server process: %w", err)
	}
	defer windows.CloseHandle(process)
	return verifyProcessImage(process, expectedExecutable)
}

func verifyProcessImage(process windows.Handle, expectedPath string) error {
	serverSID, err := processUserSID(process)
	if err != nil {
		return fmt.Errorf("read named pipe server process SID: %w", err)
	}
	currentSID, err := CurrentUserSID()
	if err != nil {
		return err
	}
	if err := verifyServerSID(serverSID, currentSID); err != nil {
		return err
	}
	expected, err := winsecurefile.OpenManagedImage(expectedPath)
	if err != nil {
		return fmt.Errorf("validate expected managed server executable: %w", err)
	}
	defer expected.Close()
	imagePath, err := processImagePath(process)
	if err != nil {
		return fmt.Errorf("read named pipe server executable: %w", err)
	}
	actual, err := winsecurefile.OpenImage(imagePath)
	if err != nil {
		return fmt.Errorf("validate named pipe server executable: %w", err)
	}
	defer actual.Close()
	same, err := winsecurefile.SameFile(expected, actual)
	if err != nil || !same {
		return fmt.Errorf("named pipe server is not running the expected managed executable")
	}
	return nil
}

func processUserSID(process windows.Handle) (string, error) {
	var token windows.Token
	if err := windows.OpenProcessToken(process, windows.TOKEN_QUERY, &token); err != nil {
		return "", err
	}
	defer token.Close()
	user, err := token.GetTokenUser()
	if err != nil {
		return "", err
	}
	return user.User.Sid.String(), nil
}

func processImagePath(process windows.Handle) (string, error) {
	buffer := make([]uint16, 32768)
	size := uint32(len(buffer))
	if err := windows.QueryFullProcessImageName(process, 0, &buffer[0], &size); err != nil {
		return "", err
	}
	return windows.UTF16ToString(buffer[:size]), nil
}
