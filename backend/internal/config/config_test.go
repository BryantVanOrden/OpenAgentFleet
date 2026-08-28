package config

import (
	"testing"
	"time"
)

// strongKey is 32 bytes in hex — the only shape production accepts.
const strongKey = "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"

// clearEnv blanks every variable Load reads so a test starts from a known state
// regardless of what is set on the developer's machine or in CI.
func clearEnv(t *testing.T) {
	t.Helper()
	for _, k := range []string{
		"AGENTFLEET_ENV", "HTTP_ADDR", "PUBLIC_URL", "DATABASE_URL", "DOCKER_HOST",
		"SANDBOX_NETWORK", "SANDBOX_IMAGE", "HOST_GATEWAY", "PUBLISH_PORTS",
		"PORT_MIN", "PORT_MAX", "MAX_INSTANCES", "MAX_CPU_OVERSUBSCRIBE",
		"TOKEN_TTL_HOURS", "JWT_SECRET", "MASTER_KEY", "ALLOW_SHELL",
		"AGENT_MAX_STEPS", "AGENT_STEP_TIMEOUT_SEC", "AGENT_STALL_THRESHOLD",
		"AGENT_STALL_DELTA", "AGENT_SCREENSHOT_MAX_WIDTH",
	} {
		t.Setenv(k, "")
	}
}

func TestLoadDevelopmentDefaults(t *testing.T) {
	clearEnv(t)

	c, err := Load()
	if err != nil {
		t.Fatalf("Load() = %v", err)
	}
	if c.Production() {
		t.Error("an empty environment should not be production")
	}
	if len(c.JWTSecret) == 0 {
		t.Error("dev JWT secret is empty")
	}
	// normaliseKey must have produced a usable AES-256 key.
	if len(c.MasterKey) != 32 {
		t.Errorf("MasterKey is %d bytes, want 32", len(c.MasterKey))
	}
	if c.HTTPAddr != ":8080" {
		t.Errorf("HTTPAddr = %q", c.HTTPAddr)
	}
	if c.PortMin >= c.PortMax {
		t.Errorf("port range %d..%d is inverted", c.PortMin, c.PortMax)
	}
	if c.MaxSteps <= 0 || c.StallThreshold <= 0 || c.ScreenshotMaxW <= 0 {
		t.Errorf("agent loop defaults are not usable: %+v", c)
	}
	if c.StepTimeout != 120*time.Second {
		t.Errorf("StepTimeout = %v, want 2m", c.StepTimeout)
	}
	if c.TokenTTL != 12*time.Hour {
		t.Errorf("TokenTTL = %v, want 12h", c.TokenTTL)
	}
}

func TestProductionRefusesInsecureDefaults(t *testing.T) {
	// The whole point of these two checks: a production deploy must never fall
	// back to the hard-coded dev secret or the dev master key.
	t.Run("no JWT secret", func(t *testing.T) {
		clearEnv(t)
		t.Setenv("AGENTFLEET_ENV", "production")
		t.Setenv("MASTER_KEY", strongKey)
		if _, err := Load(); err == nil {
			t.Fatal("Load() succeeded in production with no JWT_SECRET")
		}
	})

	t.Run("no master key", func(t *testing.T) {
		clearEnv(t)
		t.Setenv("AGENTFLEET_ENV", "production")
		t.Setenv("JWT_SECRET", "a-real-secret")
		if _, err := Load(); err == nil {
			t.Fatal("Load() succeeded in production with no MASTER_KEY")
		}
	})

	t.Run("both supplied", func(t *testing.T) {
		clearEnv(t)
		t.Setenv("AGENTFLEET_ENV", "production")
		t.Setenv("JWT_SECRET", "a-real-secret")
		t.Setenv("MASTER_KEY", strongKey)
		c, err := Load()
		if err != nil {
			t.Fatalf("Load() = %v", err)
		}
		if !c.Production() {
			t.Error("Production() = false")
		}
		if string(c.JWTSecret) != "a-real-secret" {
			t.Errorf("JWTSecret = %q", c.JWTSecret)
		}
	})
}

func TestProductionDetectionIsCaseInsensitive(t *testing.T) {
	for _, v := range []string{"production", "Production", "PRODUCTION"} {
		clearEnv(t)
		t.Setenv("AGENTFLEET_ENV", v)
		t.Setenv("JWT_SECRET", "s")
		t.Setenv("MASTER_KEY", strongKey)
		c, err := Load()
		if err != nil {
			t.Fatalf("Load() with env=%q = %v", v, err)
		}
		if !c.Production() {
			t.Errorf("Production() = false for AGENTFLEET_ENV=%q", v)
		}
	}
	for _, v := range []string{"development", "staging", "prod"} {
		clearEnv(t)
		t.Setenv("AGENTFLEET_ENV", v)
		c, err := Load()
		if err != nil {
			t.Fatalf("Load() with env=%q = %v", v, err)
		}
		if c.Production() {
			t.Errorf("Production() = true for AGENTFLEET_ENV=%q", v)
		}
	}
}

func TestLoadRejectsAnInvertedPortRange(t *testing.T) {
	for _, tc := range []struct{ min, max string }{
		{"42999", "42000"},
		{"42000", "42000"},
	} {
		clearEnv(t)
		t.Setenv("PORT_MIN", tc.min)
		t.Setenv("PORT_MAX", tc.max)
		if _, err := Load(); err == nil {
			t.Errorf("Load() accepted PORT_MIN=%s PORT_MAX=%s", tc.min, tc.max)
		}
	}
}

func TestProductionRefusesAStretchedPassphrase(t *testing.T) {
	// A passphrase is fine in development but has far less entropy than 32
	// random bytes, and MASTER_KEY seals every stored credential. Production
	// must insist on the real thing.
	for _, weak := range []string{"correct-horse-battery-staple", "hunter2", "0123456789abcdef0123456789abcdef"} {
		clearEnv(t)
		t.Setenv("AGENTFLEET_ENV", "production")
		t.Setenv("JWT_SECRET", "a-real-secret")
		t.Setenv("MASTER_KEY", weak)
		if _, err := Load(); err == nil {
			t.Errorf("Load() accepted the stretched passphrase %q in production", weak)
		}
	}
}

func TestIsFullStrengthKey(t *testing.T) {
	cases := []struct {
		name string
		raw  string
		want bool
	}{
		{"64 hex chars", strongKey, true},
		{"base64 of 32 bytes", "MDEyMzQ1Njc4OWFiY2RlZjAxMjM0NTY3ODlhYmNkZWY=", true},
		{"a 32 character passphrase is only 16 bytes of hex", "0123456789abcdef0123456789abcdef", false},
		{"a plain passphrase", "correct-horse-battery-staple", false},
		{"empty", "", false},
		{"hex of 16 bytes", "0123456789abcdef0123456789abcdef"[:32], false},
		{"base64 of 16 bytes", "MDEyMzQ1Njc4OWFiY2RlZg==", false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := isFullStrengthKey(tc.raw); got != tc.want {
				t.Errorf("isFullStrengthKey(%q) = %v, want %v", tc.raw, got, tc.want)
			}
		})
	}
}

func TestLoadStretchesAShortMasterKey(t *testing.T) {
	// normaliseKey deliberately accepts a passphrase and stretches it with
	// SHA-256, so a short MASTER_KEY is valid — but it must still come out as
	// exactly 32 bytes or the AES-256 vault cipher cannot be constructed.
	clearEnv(t)
	t.Setenv("MASTER_KEY", "too-short")
	c, err := Load()
	if err != nil {
		t.Fatalf("Load() = %v", err)
	}
	if len(c.MasterKey) != 32 {
		t.Errorf("MasterKey is %d bytes, want 32", len(c.MasterKey))
	}
}

func TestLoadAcceptsHexAndBase64MasterKeys(t *testing.T) {
	// A 32-byte key given in hex or base64 is used verbatim, not re-hashed.
	cases := map[string]string{
		"hex":    "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef",
		"base64": "MDEyMzQ1Njc4OWFiY2RlZjAxMjM0NTY3ODlhYmNkZWY=",
	}
	for name, key := range cases {
		t.Run(name, func(t *testing.T) {
			clearEnv(t)
			t.Setenv("MASTER_KEY", key)
			c, err := Load()
			if err != nil {
				t.Fatalf("Load() = %v", err)
			}
			if len(c.MasterKey) != 32 {
				t.Fatalf("MasterKey is %d bytes, want 32", len(c.MasterKey))
			}
			want, err := normaliseKey(key)
			if err != nil {
				t.Fatalf("normaliseKey: %v", err)
			}
			if string(c.MasterKey) != string(want) {
				t.Error("Load did not use the decoded key verbatim")
			}
		})
	}
}

func TestEnvHelpersFallBackOnBlankAndGarbage(t *testing.T) {
	// env() treats "" as unset, and the typed helpers fall back rather than
	// panicking on values a human fat-fingered into a .env file.
	t.Setenv("AF_TEST_VAL", "")
	if got := env("AF_TEST_VAL", "fallback"); got != "fallback" {
		t.Errorf("env(blank) = %q, want the default", got)
	}
	t.Setenv("AF_TEST_VAL", "set")
	if got := env("AF_TEST_VAL", "fallback"); got != "set" {
		t.Errorf("env() = %q, want %q", got, "set")
	}

	t.Setenv("AF_TEST_INT", "not-a-number")
	if got := envInt("AF_TEST_INT", 7); got != 7 {
		t.Errorf("envInt(garbage) = %d, want 7", got)
	}
	t.Setenv("AF_TEST_INT", "42")
	if got := envInt("AF_TEST_INT", 7); got != 42 {
		t.Errorf("envInt() = %d, want 42", got)
	}

	t.Setenv("AF_TEST_FLOAT", "nope")
	if got := envFloat("AF_TEST_FLOAT", 1.5); got != 1.5 {
		t.Errorf("envFloat(garbage) = %v, want 1.5", got)
	}
	t.Setenv("AF_TEST_FLOAT", "2.5")
	if got := envFloat("AF_TEST_FLOAT", 1.5); got != 2.5 {
		t.Errorf("envFloat() = %v, want 2.5", got)
	}

	t.Setenv("AF_TEST_BOOL", "yes-please")
	if got := envBool("AF_TEST_BOOL", true); got != true {
		t.Errorf("envBool(garbage) = %v, want the default", got)
	}
	for _, s := range []string{"false", "0", "FALSE"} {
		t.Setenv("AF_TEST_BOOL", s)
		if envBool("AF_TEST_BOOL", true) {
			t.Errorf("envBool(%q) = true, want false", s)
		}
	}
}
