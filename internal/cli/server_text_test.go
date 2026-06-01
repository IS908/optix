package cli

import (
	"strings"
	"testing"
)

func TestServerCommandTextUsesGatewayTWS(t *testing.T) {
	cmd := newServerCmd()

	if strings.Contains(cmd.Long, "IB TWS") {
		t.Fatalf("server long help still says IB TWS: %q", cmd.Long)
	}
	if !strings.Contains(cmd.Long, "IB Gateway/TWS") {
		t.Fatalf("server long help should mention IB Gateway/TWS: %q", cmd.Long)
	}

	if got := serverBrokerLabel(); got != "IB Gateway/TWS:" {
		t.Fatalf("serverBrokerLabel() = %q, want %q", got, "IB Gateway/TWS:")
	}
}
