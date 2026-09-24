package external

import (
	"context"
	"errors"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestTheAddressGuard(t *testing.T) {
	was := AllowLoopback
	AllowLoopback = false
	defer func() { AllowLoopback = was }()

	cases := []struct {
		ip           string
		allowPrivate bool
		refused      bool
	}{
		{"169.254.169.254", true, true}, // cloud metadata, whoever asks
		{"127.0.0.1", true, true},       // the orchestrator itself
		{"::1", true, true},
		{"0.0.0.0", true, true},
		{"10.25.20.223", false, true}, // a LAN gateway, for an operator
		{"10.25.20.223", true, false}, // the same, set by an admin
		{"100.101.102.103", false, true},
		{"192.168.1.20", true, false},
		{"93.184.216.34", false, false}, // the internet
	}
	for _, c := range cases {
		got := refusal(net.ParseIP(c.ip), c.allowPrivate) != ""
		if got != c.refused {
			t.Errorf("%s (allowPrivate=%v): refused=%v, want %v", c.ip, c.allowPrivate, got, c.refused)
		}
	}
}

func TestAWebhookOnLoopbackIsNotDialled(t *testing.T) {
	was := AllowLoopback
	AllowLoopback = false
	defer func() { AllowLoopback = was }()

	hit := false
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { hit = true }))
	defer srv.Close()
	req, _ := http.NewRequest(http.MethodPost, srv.URL, strings.NewReader("{}"))
	_, err := webhookClient.Do(req)
	if !errors.Is(err, ErrAddressRefused) || hit {
		t.Fatalf("a webhook pointed at this machine is refused before it is reached: err=%v hit=%v", err, hit)
	}
}

func TestCheckHost(t *testing.T) {
	was := AllowLoopback
	AllowLoopback = false
	defer func() { AllowLoopback = was }()

	if _, err := CheckHost(context.Background(), "169.254.169.254"); !errors.Is(err, ErrAddressRefused) {
		t.Errorf("the metadata address is refused when the agent is added: %v", err)
	}
	if private, err := CheckHost(context.Background(), "10.0.0.5"); err != nil || !private {
		t.Errorf("a private address is reported as private: %v %v", private, err)
	}
	if private, err := CheckHost(context.Background(), "no-such-host.invalid"); err != nil || private {
		t.Errorf("a name that does not resolve yet passes, to be checked on every dial: %v %v", private, err)
	}
}
