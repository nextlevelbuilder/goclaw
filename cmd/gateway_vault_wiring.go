package cmd

import (
	"log/slog"

	"github.com/nextlevelbuilder/goclaw/internal/eventbus"
	"github.com/nextlevelbuilder/goclaw/internal/store"
	"github.com/nextlevelbuilder/goclaw/internal/tools"
	"github.com/nextlevelbuilder/goclaw/internal/vault"
)

// wireVault wires Knowledge Vault tools and interceptors into the tool registry.
// All wiring is skipped if stores.Vault is nil.
// Pattern mirrors wireExtras KG wiring: register tools, set stores, set interceptors.
// wireVault wires Knowledge Vault tools and interceptors into the tool registry.
// Returns the shared VaultInterceptor for use by other subsystems (e.g. agent upload hook).
// Returns nil if stores.Vault is nil.
func wireVault(stores *store.Stores, toolsReg *tools.Registry, workspace string, bus eventbus.DomainEventBus) *tools.VaultInterceptor {
	if stores.Vault == nil {
		return nil
	}

	// Register vault tools — these are always available when vault store is present.
	vaultSearchTool := tools.NewVaultSearchTool()
	toolsReg.Register(vaultSearchTool)

	// Build VaultSearchService: fan-out across vault + KG (episodic store pending impl).
	// EpisodicStore is nil until a PG implementation exists.
	searchSvc := vault.NewVaultSearchService(stores.Vault, nil, stores.KnowledgeGraph)
	vaultSearchTool.SetSearchService(searchSvc)

	// Build shared VaultInterceptor for read/write tool vault registration.
	vaultIntc := tools.NewVaultInterceptor(stores.Vault, workspace, bus)

	// Wire interceptor into write_file (registers doc on write).
	if writeTool, ok := toolsReg.Get("write_file"); ok {
		if wt, ok := writeTool.(*tools.WriteFileTool); ok {
			wt.SetVaultInterceptor(vaultIntc)
		}
	}

	// Wire interceptor into read_file (lazy hash sync on read).
	if readTool, ok := toolsReg.Get("read_file"); ok {
		if rt, ok := readTool.(*tools.ReadFileTool); ok {
			rt.SetVaultInterceptor(vaultIntc)
		}
	}

	// Wire interceptor into media generation tools.
	if imgTool, ok := toolsReg.Get("create_image"); ok {
		if it, ok := imgTool.(*tools.CreateImageTool); ok {
			it.SetVaultInterceptor(vaultIntc)
		}
	}
	if vidTool, ok := toolsReg.Get("create_video"); ok {
		if vt, ok := vidTool.(*tools.CreateVideoTool); ok {
			vt.SetVaultInterceptor(vaultIntc)
		}
	}
	if audTool, ok := toolsReg.Get("create_audio"); ok {
		if at, ok := audTool.(*tools.CreateAudioTool); ok {
			at.SetVaultInterceptor(vaultIntc)
		}
	}
	if ttsTool, ok := toolsReg.Get("tts"); ok {
		if tt, ok := ttsTool.(*tools.TtsTool); ok {
			tt.SetVaultInterceptor(vaultIntc)
		}
	}
	if editTool, ok := toolsReg.Get("edit"); ok {
		if et, ok := editTool.(*tools.EditTool); ok {
			et.SetVaultInterceptor(vaultIntc)
		}
	}

	slog.Info("vault tools registered", "tools", "vault_search,create_image,create_video,create_audio,tts,edit")
	return vaultIntc
}
