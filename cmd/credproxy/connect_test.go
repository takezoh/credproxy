package main

import (
	"bytes"
	"context"
	"errors"
	"io"
	"net"
	"testing"
	"time"
)

func TestRelayStdioCopiesBinaryBytesAndRemoteEOF(t *testing.T) {
	client, server := net.Pipe()
	defer server.Close()
	request := []byte{0, 0xff, '\r', '\n', 0x80}
	response := []byte{0xfe, 0, '\n', 0x81}
	serverDone := make(chan error, 1)
	go func() {
		actual := make([]byte, len(request))
		if _, err := io.ReadFull(server, actual); err != nil {
			serverDone <- err
			return
		}
		if !bytes.Equal(actual, request) {
			serverDone <- errors.New("request bytes changed in transport")
			return
		}
		_, err := server.Write(response)
		serverDone <- errors.Join(err, server.Close())
	}()
	var output bytes.Buffer
	if err := relayStdio(context.Background(), client, bytes.NewReader(request), &output); err != nil {
		t.Fatal(err)
	}
	if err := <-serverDone; err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(output.Bytes(), response) {
		t.Fatalf("response bytes = %v, want %v", output.Bytes(), response)
	}
}

func TestRelayStdioReturnsOutputErrors(t *testing.T) {
	client, server := net.Pipe()
	defer server.Close()
	go func() {
		request := make([]byte, len("request"))
		_, _ = io.ReadFull(server, request)
		_, _ = server.Write([]byte("response"))
	}()
	want := errors.New("output blocked")
	err := relayStdio(context.Background(), client, bytes.NewReader([]byte("request")), failingWriter{err: want})
	if !errors.Is(err, want) {
		t.Fatalf("relay error = %v, want %v", err, want)
	}
}

func TestRelayStdioCancellationClosesConnection(t *testing.T) {
	client, server := net.Pipe()
	defer server.Close()
	input, inputWriter := io.Pipe()
	defer inputWriter.Close()
	ctx, cancel := context.WithCancel(context.Background())
	finished := make(chan error, 1)
	go func() { finished <- relayStdio(ctx, client, input, io.Discard) }()
	cancel()
	select {
	case err := <-finished:
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("relay error = %v, want cancellation", err)
		}
	case <-time.After(time.Second):
		t.Fatal("relay did not stop after cancellation")
	}
}

type failingWriter struct{ err error }

func (w failingWriter) Write([]byte) (int, error) { return 0, w.err }
