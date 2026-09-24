// Command server is the OpenAgentFleet orchestrator: fleet provisioning, the agent
// loop, the model gateway and the API the admin panel and companion app talk to.
package main

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/BryantVanOrden/OpenAgentFleet/backend/internal/agent"
	"github.com/BryantVanOrden/OpenAgentFleet/backend/internal/artifacts"
	"github.com/BryantVanOrden/OpenAgentFleet/backend/internal/bus"
	"github.com/BryantVanOrden/OpenAgentFleet/backend/internal/config"
	"github.com/BryantVanOrden/OpenAgentFleet/backend/internal/connectors"
	"github.com/BryantVanOrden/OpenAgentFleet/backend/internal/fleet"
	"github.com/BryantVanOrden/OpenAgentFleet/backend/internal/httpapi"
	"github.com/BryantVanOrden/OpenAgentFleet/backend/internal/notify"
	"github.com/BryantVanOrden/OpenAgentFleet/backend/internal/store"
	"github.com/BryantVanOrden/OpenAgentFleet/backend/internal/vault"
	"github.com/BryantVanOrden/OpenAgentFleet/backend/pkg/protocol"
)

func main() {
	log := newLogger()

	if err := run(log); err != nil {
		log.Error("fatal", "err", err)
		os.Exit(1)
	}
}

func run(log *slog.Logger) error {
	cfg, err := config.Load()
	if err != nil {
		return err
	}
	log.Info("starting agentfleet", "env", cfg.Env, "addr", cfg.HTTPAddr)

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	// --- persistence ---
	db, err := store.Open(ctx, cfg.DatabaseURL)
	if err != nil {
		return err
	}
	defer db.Close()
	if err := db.Migrate(ctx); err != nil {
		return err
	}
	log.Info("database ready")

	// --- credential vault ---
	v, err := vault.New(cfg.MasterKey, db)
	if err != nil {
		return err
	}

	// --- artifact storage ---
	art, err := openArtifacts(ctx, cfg, log)
	if err != nil {
		return err
	}

	// --- fleet ---
	fm, err := fleet.NewManager(cfg, db, log)
	if err != nil {
		return err
	}
	fm.Reconcile(ctx)

	// --- model gateway ---
	models := connectors.NewRegistry(db, v, log)
	// Lets a chain entry name a combination rather than a single provider, so
	// each kind of thinking reaches the model assigned to it.
	models.AttachCombos(db)
	if err := seedProviders(ctx, db, cfg, log); err != nil {
		log.Warn("provider seeding skipped", "err", err)
	}

	// --- events, push, agent loop ---
	eventBus := bus.New()
	pusher := notify.NewDispatcher(cfg, db, log)
	runner := agent.NewRunner(cfg, db, models, eventBus, pusher, art, log)
	runner.SetFilePlacer(fm)

	if created, err := httpapi.EnsureBootstrapUser(ctx, db,
		os.Getenv("BOOTSTRAP_ADMIN_EMAIL"), os.Getenv("BOOTSTRAP_ADMIN_PASSWORD")); err != nil {
		log.Warn("bootstrap admin not created", "err", err)
	} else if created {
		log.Info("bootstrap admin created", "email", os.Getenv("BOOTSTRAP_ADMIN_EMAIL"))
	}

	// Background loops.
	go fm.WatchStats(ctx, 10*time.Second, func(s protocol.InstanceStats) {
		eventBus.Emit("stats", s.InstanceID, "", s)
	})
	go reconcileLoop(ctx, fm, 30*time.Second)

	// --- http ---
	api := httpapi.NewServer(cfg, db, fm, models, runner, eventBus, v, art, log)
	// After NewServer, which connects the external-agent adapters: resumed
	// before it, a Claude Code run interrupted by a restart was put through
	// the desktop loop and failed trying to screenshot a desktop it has not got.
	runner.ResumeInterrupted(ctx)
	// Loads the persisted triggers, peer messages and episodic memory, and
	// starts the cron engine. Before serving, so a request cannot arrive
	// against half-loaded configuration.
	api.StartBackground(ctx)

	srv := &http.Server{
		Addr:    cfg.HTTPAddr,
		Handler: api.Routes(),
		// No WriteTimeout: the event stream and the desktop proxy are long-lived.
		ReadHeaderTimeout: 15 * time.Second,
		IdleTimeout:       120 * time.Second,
	}

	errc := make(chan error, 1)
	go func() {
		log.Info("listening", "addr", cfg.HTTPAddr)
		if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			errc <- err
		}
	}()

	select {
	case err := <-errc:
		return err
	case <-ctx.Done():
		log.Info("shutting down")
	}

	shutdownCtx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	return srv.Shutdown(shutdownCtx)
}

func reconcileLoop(ctx context.Context, fm *fleet.Manager, every time.Duration) {
	t := time.NewTicker(every)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			fm.Reconcile(ctx)
		}
	}
}

// openArtifacts prefers S3/MinIO when an endpoint and credentials are present,
// and otherwise keeps screenshots on a local volume so a bare `docker compose up`
// still produces a full audit trail.
func openArtifacts(ctx context.Context, cfg *config.Config, log *slog.Logger) (artifacts.Store, error) {
	if strings.EqualFold(os.Getenv("ARTIFACT_BACKEND"), "s3") {
		s3 := artifacts.NewS3Store(artifacts.S3Options{
			Endpoint:  cfg.S3Endpoint,
			Bucket:    cfg.S3Bucket,
			Region:    cfg.S3Region,
			AccessKey: cfg.S3AccessKey,
			SecretKey: cfg.S3SecretKey,
			PathStyle: true, // MinIO requires it; AWS tolerates it
		})
		bctx, cancel := context.WithTimeout(ctx, 15*time.Second)
		defer cancel()
		if err := s3.EnsureBucket(bctx); err != nil {
			return nil, err
		}
		log.Info("artifact storage: s3", "endpoint", cfg.S3Endpoint, "bucket", cfg.S3Bucket)
		return s3, nil
	}
	root := os.Getenv("ARTIFACT_DIR")
	if root == "" {
		root = "/var/lib/agentfleet/artifacts"
	}
	fs, err := artifacts.NewFSStore(root)
	if err != nil {
		return nil, err
	}
	log.Info("artifact storage: filesystem", "root", root)
	return fs, nil
}

// seedProviders registers a local Ollama endpoint on first boot so a fresh
// install has something to run against without any cloud key.
func seedProviders(ctx context.Context, db *store.Store, cfg *config.Config, log *slog.Logger) error {
	existing, err := db.ListProviders(ctx, false)
	if err != nil {
		return err
	}
	if len(existing) > 0 {
		return nil
	}
	base := os.Getenv("OLLAMA_BASE_URL")
	if base == "" {
		base = "http://" + cfg.HostGateway + ":11434"
	}
	model := os.Getenv("OLLAMA_VISION_MODEL")
	if model == "" {
		model = "qwen3.5:4b"
	}
	p := &protocol.Provider{
		Name:        "Local Ollama",
		Kind:        protocol.ProviderOllama,
		BaseURL:     base,
		Model:       model,
		Vision:      true,
		Temperature: 0.2,
		MaxTokens:   1024,
		Priority:    10,
		Enabled:     true,
	}
	if err := db.UpsertProvider(ctx, p); err != nil {
		return err
	}
	log.Info("seeded default provider", "name", p.Name, "base", base, "model", model)
	return nil
}

func newLogger() *slog.Logger {
	level := slog.LevelInfo
	if strings.EqualFold(os.Getenv("LOG_LEVEL"), "debug") {
		level = slog.LevelDebug
	}
	if strings.EqualFold(os.Getenv("LOG_FORMAT"), "json") {
		return slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{Level: level}))
	}
	return slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: level}))
}
