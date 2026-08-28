// Package notify pushes alerts to the Flutter companion app.
//
// FCM handles Android (and iOS too, if the app is wired to Firebase); the APNs
// path exists for builds that talk to Apple directly. When neither is
// configured the dispatcher degrades to bus-only delivery, which is what a
// local development setup wants — the admin panel still lights up.
package notify

import (
	"context"
	"log/slog"

	"github.com/BryantVanOrden/AgentFleet/backend/internal/config"
	"github.com/BryantVanOrden/AgentFleet/backend/pkg/protocol"
)

// DeviceSource supplies the tokens to push to.
type DeviceSource interface {
	PushTargets(ctx context.Context, userID string) ([]string, []string, error)
	DeleteDevice(ctx context.Context, token string) error
}

// Dispatcher fans one alert out to every registered device.
type Dispatcher struct {
	devices DeviceSource
	fcm     *FCM
	apns    *APNs
	log     *slog.Logger
}

func NewDispatcher(cfg *config.Config, devices DeviceSource, log *slog.Logger) *Dispatcher {
	d := &Dispatcher{devices: devices, log: log}

	if cfg.FCMProjectID != "" && cfg.FCMServiceAccountPath != "" {
		f, err := NewFCM(cfg.FCMProjectID, cfg.FCMServiceAccountPath)
		if err != nil {
			log.Error("FCM disabled", "err", err)
		} else {
			d.fcm = f
			log.Info("FCM push enabled", "project", cfg.FCMProjectID)
		}
	}
	if cfg.APNsKeyPath != "" && cfg.APNsKeyID != "" && cfg.APNsTeamID != "" {
		a, err := NewAPNs(cfg.APNsTeamID, cfg.APNsKeyID, cfg.APNsKeyPath, cfg.APNsTopic, cfg.Production())
		if err != nil {
			log.Error("APNs disabled", "err", err)
		} else {
			d.apns = a
			log.Info("APNs push enabled", "topic", cfg.APNsTopic)
		}
	}
	if d.fcm == nil && d.apns == nil {
		log.Warn("no push provider configured; alerts are delivered in-app only")
	}
	return d
}

// Send delivers an alert. Errors on individual devices are logged and swallowed:
// a dead phone token must never fail a task.
func (d *Dispatcher) Send(ctx context.Context, a protocol.Alert) error {
	android, ios, err := d.devices.PushTargets(ctx, "")
	if err != nil {
		return err
	}

	payload := map[string]string{
		"alert_id":    a.ID,
		"kind":        string(a.Kind),
		"severity":    a.Severity,
		"instance_id": a.InstanceID,
		"task_id":     a.TaskID,
		"needs_reply": boolStr(a.NeedsReply),
	}

	if d.fcm != nil {
		for _, token := range android {
			if err := d.fcm.Send(ctx, token, a.Title, a.Body, payload, a.Severity); err != nil {
				d.handleTokenError(ctx, token, err)
			}
		}
	}
	if d.apns != nil {
		for _, token := range ios {
			if err := d.apns.Send(ctx, token, a.Title, a.Body, payload, a.NeedsReply); err != nil {
				d.handleTokenError(ctx, token, err)
			}
		}
	} else if d.fcm != nil {
		// Firebase-routed iOS: the app registered an FCM token, not an APNs one.
		for _, token := range ios {
			if err := d.fcm.Send(ctx, token, a.Title, a.Body, payload, a.Severity); err != nil {
				d.handleTokenError(ctx, token, err)
			}
		}
	}
	return nil
}

func (d *Dispatcher) handleTokenError(ctx context.Context, token string, err error) {
	if IsUnregistered(err) {
		d.log.Info("pruning dead push token")
		_ = d.devices.DeleteDevice(ctx, token)
		return
	}
	d.log.Warn("push failed", "err", err)
}

func boolStr(b bool) string {
	if b {
		return "true"
	}
	return "false"
}
