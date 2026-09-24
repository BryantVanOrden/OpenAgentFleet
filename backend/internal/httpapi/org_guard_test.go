package httpapi

import (
	"context"
	"strings"
	"testing"
)

// Who may point a webhook or OpenClaw agent where.
func TestCheckAgentHost(t *testing.T) {
	ctx := context.Background()
	if _, err := checkAgentHost(ctx, "169.254.169.254", true); err == nil {
		t.Error("not even an admin can point an agent at the cloud metadata address")
	}
	if _, err := checkAgentHost(ctx, "10.25.20.223", false); err == nil || !strings.Contains(err.Error(), "only an admin") {
		t.Errorf("an operator cannot point an agent at the private network: %v", err)
	}
	if private, err := checkAgentHost(ctx, "10.25.20.223", true); err != nil || !private {
		t.Errorf("an admin can, and the agent is marked private: %v %v", private, err)
	}
	if private, err := checkAgentHost(ctx, "93.184.216.34", false); err != nil || private {
		t.Errorf("anyone can point one at the internet: %v %v", private, err)
	}
}
