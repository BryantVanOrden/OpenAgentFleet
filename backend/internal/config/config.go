// Package config loads runtime configuration from the environment. Every value
// has a development-safe default so the stack boots with an empty .env, but the
// server refuses to start in production without an explicit JWT secret and
// master key.
package config

import (
	"errors"
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"
)

type Config struct {
	Env       string // development | production
	HTTPAddr  string
	PublicURL string

	DatabaseURL string

	// Docker daemon endpoint. On Linux this is the unix socket; on Windows the
	// named pipe; a tcp:// URL works for remote hosts.
	DockerHost string

	// SandboxNetwork is the docker network sandboxes are attached to. It is
	// created on boot if missing and is where egress policy is enforced.
	SandboxNetwork string
	SandboxImage   string
	// HostGateway is how a sandbox reaches services on the host (Ollama, etc).
	HostGateway string

	// PublishPorts binds each sandbox's noVNC and agentd ports to loopback on
	// the host. Off by default: clients reach sandboxes through the
	// authenticated reverse proxy on the orchestrator instead.
	PublishPorts bool
	// PortRange is the host port band handed out when PublishPorts is on.
	PortMin int
	PortMax int

	// MaxInstances caps concurrent sandboxes across the whole host.
	MaxInstances int
	// MaxCPUOversubscribe multiplies host cores when admitting instances.
	MaxCPUOversubscribe float64

	JWTSecret []byte
	TokenTTL  time.Duration
	MasterKey []byte // AES-256 key for the credential vault

	// Object storage for screenshots, session video and build artefacts.
	S3Endpoint  string
	S3Bucket    string
	S3AccessKey string
	S3SecretKey string
	S3Region    string
	S3UseSSL    bool

	// Push notifications.
	FCMProjectID          string
	FCMServiceAccountPath string
	APNsTeamID            string
	APNsKeyID             string
	APNsKeyPath           string
	APNsTopic             string

	// Agent loop defaults.
	MaxSteps       int
	StepTimeout    time.Duration
	StallThreshold int     // identical observations before intervening
	StallDelta     float64 // fraction of changed pixels considered "movement"
	ScreenshotMaxW int

	// CoordSpace is the coordinate convention the vision model answers in.
	// "auto" (default) measures the configured model once with a calibration
	// frame and caches the answer; "pixel" and "normalized" pin it.
	//
	// This is not a preference the prompt can express. Qwen3/Qwen3.5-class
	// models emit normalised coordinates no matter what the prompt says --
	// measured: identical output when told the pixel range explicitly and even
	// when shown a worked example -- so the conversion has to happen on our
	// side, and which side a given model is on has to be discovered.
	CoordSpace string

	// AllowShell is the global kill switch for the shell action. Individual
	// instances still have to opt in on top of this.
	AllowShell bool
}

func Load() (*Config, error) {
	c := &Config{
		Env:                   env("AGENTFLEET_ENV", "development"),
		HTTPAddr:              env("HTTP_ADDR", ":8080"),
		PublicURL:             env("PUBLIC_URL", "http://localhost:8080"),
		DatabaseURL:           env("DATABASE_URL", "postgres://agentfleet:agentfleet@localhost:5432/agentfleet?sslmode=disable"),
		DockerHost:            env("DOCKER_HOST", defaultDockerHost()),
		SandboxNetwork:        env("SANDBOX_NETWORK", "agentfleet-sandbox"),
		SandboxImage:          env("SANDBOX_IMAGE", "agentfleet/sandbox:latest"),
		HostGateway:           env("HOST_GATEWAY", "host.docker.internal"),
		PublishPorts:          envBool("PUBLISH_PORTS", false),
		PortMin:               envInt("PORT_MIN", 42000),
		PortMax:               envInt("PORT_MAX", 42999),
		MaxInstances:          envInt("MAX_INSTANCES", 16),
		MaxCPUOversubscribe:   envFloat("MAX_CPU_OVERSUBSCRIBE", 2.0),
		TokenTTL:              time.Duration(envInt("TOKEN_TTL_HOURS", 12)) * time.Hour,
		S3Endpoint:            env("S3_ENDPOINT", "http://localhost:9000"),
		S3Bucket:              env("S3_BUCKET", "agentfleet"),
		S3AccessKey:           env("S3_ACCESS_KEY", "minioadmin"),
		S3SecretKey:           env("S3_SECRET_KEY", "minioadmin"),
		S3Region:              env("S3_REGION", "us-east-1"),
		S3UseSSL:              envBool("S3_USE_SSL", false),
		FCMProjectID:          env("FCM_PROJECT_ID", ""),
		FCMServiceAccountPath: env("FCM_SERVICE_ACCOUNT", ""),
		APNsTeamID:            env("APNS_TEAM_ID", ""),
		APNsKeyID:             env("APNS_KEY_ID", ""),
		APNsKeyPath:           env("APNS_KEY_PATH", ""),
		APNsTopic:             env("APNS_TOPIC", ""),
		MaxSteps:              envInt("AGENT_MAX_STEPS", 60),
		StepTimeout:           time.Duration(envInt("AGENT_STEP_TIMEOUT_SEC", 120)) * time.Second,
		StallThreshold:        envInt("AGENT_STALL_THRESHOLD", 3),
		StallDelta:            envFloat("AGENT_STALL_DELTA", 0.02),
		ScreenshotMaxW:        envInt("AGENT_SCREENSHOT_MAX_WIDTH", 1280),
		CoordSpace:            env("AGENT_COORD_SPACE", "auto"),
		AllowShell:            envBool("ALLOW_SHELL", true),
	}

	secret := env("JWT_SECRET", "")
	if secret == "" {
		if c.Production() {
			return nil, errors.New("JWT_SECRET is required in production")
		}
		secret = "dev-insecure-jwt-secret-change-me"
	}
	c.JWTSecret = []byte(secret)

	master := env("MASTER_KEY", "")
	if master == "" {
		if c.Production() {
			return nil, errors.New("MASTER_KEY is required in production (32 bytes, base64 or hex)")
		}
		master = "dev-insecure-master-key-0123456789abcdef"
	}
	// In production the key must be a real 32-byte value, base64 or hex. A
	// passphrase is stretched with a single SHA-256 pass, which is fine for a
	// laptop but is not a key — it is brute-forceable, and it seals every stored
	// credential. Development still accepts one so `make up` works with no
	// ceremony.
	if c.Production() && !isFullStrengthKey(master) {
		return nil, errors.New(
			"MASTER_KEY must be exactly 32 bytes encoded as base64 or hex in production; " +
				"generate one with: openssl rand -base64 32")
	}
	key, err := normaliseKey(master)
	if err != nil {
		return nil, fmt.Errorf("MASTER_KEY: %w", err)
	}
	c.MasterKey = key

	if c.PortMin >= c.PortMax {
		return nil, errors.New("PORT_MIN must be below PORT_MAX")
	}
	return c, nil
}

func (c *Config) Production() bool { return strings.EqualFold(c.Env, "production") }

func defaultDockerHost() string {
	if os.Getenv("OS") == "Windows_NT" {
		return "npipe:////./pipe/docker_engine"
	}
	return "unix:///var/run/docker.sock"
}

func env(k, def string) string {
	if v, ok := os.LookupEnv(k); ok && v != "" {
		return v
	}
	return def
}

func envInt(k string, def int) int {
	if v, err := strconv.Atoi(env(k, "")); err == nil {
		return v
	}
	return def
}

func envFloat(k string, def float64) float64 {
	if v, err := strconv.ParseFloat(env(k, ""), 64); err == nil {
		return v
	}
	return def
}

func envBool(k string, def bool) bool {
	if v, err := strconv.ParseBool(env(k, "")); err == nil {
		return v
	}
	return def
}
