package ibkr

import (
	"bytes"
	"context"
	"log"
	"strings"
	"testing"
	"time"

	"github.com/rs/zerolog"
	"github.com/scmhub/ibapi"
)

func TestWatchDisconnectSuppressesIntentionalDisconnectWarning(t *testing.T) {
	c := &Client{
		cfg:           Config{ClientID: 42},
		wrapper:       newIbWrapper(),
		connected:     true,
		disconnecting: true,
	}
	var logs bytes.Buffer
	oldOut := log.Writer()
	log.SetOutput(&logs)
	t.Cleanup(func() { log.SetOutput(oldOut) })

	done := make(chan struct{})
	go func() {
		c.watchDisconnect(context.Background())
		close(done)
	}()
	c.wrapper.ConnectionClosed()

	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("watchDisconnect did not return")
	}
	if c.IsConnected() {
		t.Fatal("client still connected after disconnect signal")
	}
	if strings.Contains(logs.String(), "TCP connection dropped") {
		t.Fatalf("intentional disconnect logged as TCP drop: %s", logs.String())
	}
}

func TestWatchDisconnectLogsUnexpectedDrop(t *testing.T) {
	c := &Client{
		cfg:       Config{ClientID: 43},
		wrapper:   newIbWrapper(),
		connected: true,
	}
	var logs bytes.Buffer
	oldOut := log.Writer()
	log.SetOutput(&logs)
	t.Cleanup(func() { log.SetOutput(oldOut) })

	done := make(chan struct{})
	go func() {
		c.watchDisconnect(context.Background())
		close(done)
	}()
	c.wrapper.ConnectionClosed()

	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("watchDisconnect did not return")
	}
	if c.IsConnected() {
		t.Fatal("client still connected after disconnect signal")
	}
	if !strings.Contains(logs.String(), "TCP connection dropped (clientID 43)") {
		t.Fatalf("unexpected drop was not logged: %s", logs.String())
	}
}

func TestConfigureIBAPILoggerSuppressesProtocolInfo(t *testing.T) {
	oldLogger := *ibapi.Logger()
	t.Cleanup(func() { ibapi.SetLogger(oldLogger) })

	var logs bytes.Buffer
	ibapi.SetLogger(zerolog.New(&logs))

	configureIBAPILogger()
	ibapi.Logger().Info().Msg("protocol noise")

	if logs.Len() != 0 {
		t.Fatalf("ibapi info log leaked after quiet configuration: %q", logs.String())
	}
}
