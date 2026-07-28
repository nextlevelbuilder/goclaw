package cmd

import (
	"context"

	"github.com/nextlevelbuilder/goclaw/internal/bus"
	"github.com/nextlevelbuilder/goclaw/internal/store"
	"github.com/nextlevelbuilder/goclaw/internal/teamworkconfig"
)

// teamWorkConfigInvalidatorSubID is the bus subscriber ID for the Team Work
// config cache invalidator. It is deliberately distinct from the shared-config
// refresh subscriber (which registers under bus.TopicSystemConfigChanged) so the
// two coexist rather than one overwriting the other in the bus subscriber map.
const teamWorkConfigInvalidatorSubID = "teamworkconfig:invalidate"

// registerTeamWorkConfigInvalidator wires the process-local bus subscriber that
// drops a tenant's cached Team Work classifier settings when that tenant's
// system_configs change, so the next request for that tenant re-reads its
// overrides from the store.
//
// This is the LOCAL-SUBSCRIBER half of the single-process invalidation model
// (Phase 7 review 7B-H3). GoClaw runs a single gateway process, so an in-process
// MessageBus event is sufficient to keep the one resolver cache coherent — there
// is no second replica holding a competing cache. A multi-replica gateway would
// instead need a distributed invalidation signal or a bounded TTL; see the
// teamworkconfig package doc.
//
// MessageBus.Broadcast fans every event out to every subscriber (there is no
// per-topic routing), so the handler MUST filter by event name and act only on
// system-config changes — otherwise it would invalidate on unrelated events. The
// tenant is taken from the event payload context (the request that triggered the
// change); a payload without a context falls back to the master tenant, matching
// the shared-config refresh subscriber's tenant resolution.
func registerTeamWorkConfigInvalidator(msgBus *bus.MessageBus, resolver *teamworkconfig.Resolver) {
	if msgBus == nil || resolver == nil {
		return
	}
	msgBus.Subscribe(teamWorkConfigInvalidatorSubID, func(evt bus.Event) {
		if evt.Name != bus.TopicSystemConfigChanged {
			return
		}
		ctx := context.Background()
		if reqCtx, ok := evt.Payload.(context.Context); ok {
			ctx = reqCtx
		} else {
			ctx = store.WithTenantID(ctx, store.MasterTenantID)
		}
		resolver.Invalidate(store.TenantIDFromContext(ctx))
	})
}
