package extplugin

import (
	"context"

	"github.com/7K-Inari/inari-ext-argocd/internal/gateway"
)

// health reports NOT_SERVING when the Agent Gateway is not configured, so the
// host can surface a degraded extension instead of silent failures.
type health struct {
	gw gateway.Gateway
}

func (h health) Health(_ context.Context) (bool, string) {
	if h.gw == nil {
		return false, "agent gateway is not configured; actions fail closed"
	}
	return true, "agent gateway configured"
}
