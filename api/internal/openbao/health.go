package openbao

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"time"
)

// Health is the part of OpenBao's /v1/sys/health response we care about.
// (Field names verified against openbao/api/sys_health.go, v2.7.1.)
type Health struct {
	Initialized bool   `json:"initialized"`
	Sealed      bool   `json:"sealed"`
	Standby     bool   `json:"standby"`
	Version     string `json:"version"`
	ClusterName string `json:"cluster_name"`
}

// HealthChecker asks one OpenBao server "are you healthy?".
// It deliberately uses plain net/http (no token, no client library):
// /v1/sys/health is unauthenticated so load balancers can call it.
type HealthChecker struct {
	addr   string
	client *http.Client
}

// NewHealthChecker builds a checker with a timeout, so a hung OpenBao
// can't make our own health endpoint hang.
func NewHealthChecker(addr string, timeout time.Duration) *HealthChecker {
	return &HealthChecker{addr: addr, client: &http.Client{Timeout: timeout}}
}

// Check calls GET /v1/sys/health. Default status codes:
//
//	200 = initialized, unsealed, active     ← the only "ready" answer
//	429 = unsealed standby node (M5's cluster)
//	503 = sealed
//	501 = not initialized
func (h *HealthChecker) Check(ctx context.Context) (*Health, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, h.addr+"/v1/sys/health", nil)
	if err != nil {
		return nil, fmt.Errorf("building request: %w", err)
	}

	resp, err := h.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("cannot reach OpenBao at %s: %w", h.addr, err)
	}
	defer resp.Body.Close()

	var health Health
	if err := json.NewDecoder(resp.Body).Decode(&health); err != nil {
		return nil, fmt.Errorf("reading OpenBao response: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		return &health, fmt.Errorf("OpenBao not ready (HTTP %d, initialized=%t, sealed=%t)",
			resp.StatusCode, health.Initialized, health.Sealed)
	}
	return &health, nil
}
