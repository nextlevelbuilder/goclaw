package bitrix24

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"

	"github.com/google/uuid"

	"github.com/nextlevelbuilder/goclaw/internal/store"
)

// DestroyOrphanBot is the lazy-load path for unregistering a bot when the
// channel is no longer loaded in the runtime Manager (e.g. it was disabled
// via enabled=false). The standard Channel.Destroy path is preferred and
// runs when the channel is still loaded; this function fills the gap when
// the InstanceLoader's Reload already unregistered the channel from
// channels.Manager.
//
// Scenario this exists for (otherwise: zombie bot):
//  1. Admin disables a bitrix24 channel via UI.
//  2. InstanceLoader.Reload → ListAllEnabled excludes disabled rows →
//     manager.UnregisterChannel(name) → GetChannel returns false.
//  3. Admin deletes the channel.
//  4. handleDelete's standard destroyer block sees no channel in Manager →
//     would skip cleanup. WITHOUT this function the bot lives on at Bitrix.
//
// Implementation: load the portal directly from the store, look up bot_id
// via the persisted RegisteredBots map, fire imbot.unregister, then forget
// the mapping. Each step is best-effort + idempotent.
//
// Returns nil on no-op cases (config missing fields, no bot registered) so
// callers can wrap with a simple `if err := ...; err != nil` and only see
// real failures (store read, JSON decode).
func DestroyOrphanBot(
	ctx context.Context,
	portalStore store.BitrixPortalStore,
	encKey string,
	tenantID uuid.UUID,
	configJSON []byte,
) error {
	if portalStore == nil {
		return fmt.Errorf("bitrix24 orphan destroy: nil portal store")
	}
	if len(configJSON) == 0 {
		return nil // nothing to do
	}

	var cfg struct {
		Portal  string `json:"portal"`
		BotCode string `json:"bot_code"`
	}
	if err := json.Unmarshal(configJSON, &cfg); err != nil {
		return fmt.Errorf("bitrix24 orphan destroy: decode config: %w", err)
	}
	if cfg.Portal == "" || cfg.BotCode == "" {
		return nil // missing required fields → channel was never functional
	}

	portal, err := NewPortal(ctx, tenantID, cfg.Portal, portalStore, encKey)
	if err != nil {
		return fmt.Errorf("bitrix24 orphan destroy: load portal %q: %w", cfg.Portal, err)
	}

	botID, ok := portal.LookupRegisteredBot(cfg.BotCode)
	if !ok || botID <= 0 {
		return nil // no bot was ever registered for this code
	}

	if _, callErr := portal.Client().Call(ctx, "imbot.unregister", map[string]any{
		"BOT_ID": botID,
	}); callErr != nil {
		if !isBotNotFoundError(callErr) {
			slog.Warn("bitrix24 orphan destroy: imbot.unregister failed",
				"tenant", tenantID, "portal", cfg.Portal,
				"bot_code", cfg.BotCode, "bot_id", botID, "err", callErr)
		} else {
			slog.Info("bitrix24 orphan destroy: bot already absent on portal — treating as success",
				"tenant", tenantID, "portal", cfg.Portal, "bot_code", cfg.BotCode, "bot_id", botID)
		}
	}

	// Clear the persisted mapping regardless of whether the API call
	// succeeded — the bot is either gone now (API success) or was already
	// gone (BOT_NOT_FOUND). Best-effort: persist failure logged but not
	// fatal; next manual cleanup or reinstall will catch it.
	if err := portal.ForgetRegisteredBot(ctx, cfg.BotCode); err != nil {
		slog.Warn("bitrix24 orphan destroy: ForgetRegisteredBot failed",
			"tenant", tenantID, "portal", cfg.Portal,
			"bot_code", cfg.BotCode, "err", err)
	}
	return nil
}
