package cmd

import (
	"log/slog"

	"github.com/nextlevelbuilder/goclaw/internal/store"
	"github.com/nextlevelbuilder/goclaw/internal/tools"
	"github.com/nextlevelbuilder/goclaw/internal/vault"
)

// wireVault wires Knowledge Vault tools and interceptors into the tool registry.
// All wiring is skipped if stores.Vault is nil.
// Pattern mirrors wireExtras KG wiring: register tools, set stores, set interceptors.
func wireVault(stores *store.Stores, toolsReg *tools.Registry, workspace string) {
	if stores.Vault == nil {
		return
	}

	// Register vault tools — these are always available when vault store is present.
	vaultSearchTool := tools.NewVaultSearchTool()
	vaultLinkTool := tools.NewVaultLinkTool()
	vaultBacklinksTool := tools.NewVaultBacklinksTool()
	toolsReg.Register(vaultSearchTool)
	toolsReg.Register(vaultLinkTool)
	toolsReg.Register(vaultBacklinksTool)

	// Wire vault store onto link/backlinks tools.
	vaultLinkTool.SetVaultStore(stores.Vault)
	vaultBacklinksTool.SetVaultStore(stores.Vault)

	// Build VaultSearchService: fan-out across vault + KG (episodic store pending impl).
	// EpisodicStore is nil until a PG implementation exists.
	searchSvc := vault.NewVaultSearchService(stores.Vault, nil, stores.KnowledgeGraph)
	vaultSearchTool.SetSearchService(searchSvc)

	// Build shared VaultInterceptor for read/write tool vault registration.
	vaultIntc := tools.NewVaultInterceptor(stores.Vault, workspace)

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

	slog.Info("vault tools registered", "tools", "vault_search,vault_link,vault_backlinks")
}
