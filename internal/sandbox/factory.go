package sandbox

import (
	"context"
	"fmt"
)

// NewManager creates the appropriate sandbox manager based on runtime config.
func NewManager(ctx context.Context, cfg Config) (Manager, error) {
	switch cfg.Runtime {
	case RuntimeK8s:
		clientset, restCfg, err := CheckK8sAvailable(ctx, cfg.KubeconfigPath)
		if err != nil {
			return nil, fmt.Errorf("k8s not available: %w", err)
		}
		return NewK8sManager(cfg, clientset, restCfg), nil
	default: // RuntimeDocker or empty
		if err := CheckDockerAvailable(ctx); err != nil {
			return nil, fmt.Errorf("docker not available: %w", err)
		}
		return NewDockerManager(cfg), nil
	}
}
