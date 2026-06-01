package adapter

import (
	"context"
	"hash/fnv"

	"github.com/sirupsen/logrus"
	"github.com/spf13/viper"
)

// ─── Router: grayscale traffic control (T24) ─────────────────────────────────
//
// Configuration (hot-reloadable via viper.WatchConfig):
//
//   feature_flags:
//     use_thingmodel:
//       enabled: false
//       rollout_percentage: 5
//       whitelisted_tenants: ["tenant-A"]
//       blacklisted_tenants: ["tenant-zzz"]

// Router decides whether a given tenant should be served by the new
// device-metadata service or the legacy data path.
// All config reads go through viper so changes are picked up without restart.
type Router struct{}

// DefaultRouter is the singleton instance for production use.
var DefaultRouter = &Router{}

// ShouldUseThingModel returns true when the tenant should be routed to the
// new device-metadata service for this request.
//
// Decision order:
//  1. Global kill-switch (`enabled: false`) → always false.
//  2. Tenant in blacklist → always false.
//  3. Tenant in whitelist → always true.
//  4. FNV hash of tenant_id modulo 100 < rollout_percentage → true.
//
// The FNV hash guarantees the same tenant always lands in the same bucket,
// preventing a single tenant from seeing inconsistent behaviour across
// requests or server restarts.
func (r *Router) ShouldUseThingModel(_ context.Context, tenantID string) bool {
	if !viper.GetBool("feature_flags.use_thingmodel.enabled") {
		return false
	}

	// Blacklist takes priority over whitelist
	for _, t := range viper.GetStringSlice("feature_flags.use_thingmodel.blacklisted_tenants") {
		if t == tenantID {
			logrus.WithField("tenant_id", tenantID).Debug("[router] tenant blacklisted, using legacy path")
			return false
		}
	}

	for _, t := range viper.GetStringSlice("feature_flags.use_thingmodel.whitelisted_tenants") {
		if t == tenantID {
			logrus.WithField("tenant_id", tenantID).Debug("[router] tenant whitelisted, using thingmodel path")
			return true
		}
	}

	pct := viper.GetInt("feature_flags.use_thingmodel.rollout_percentage")
	if pct <= 0 {
		return false
	}
	if pct >= 100 {
		return true
	}

	bucket := hashTenantBucket(tenantID)
	result := bucket < pct

	logrus.WithFields(logrus.Fields{
		"tenant_id":  tenantID,
		"bucket":     bucket,
		"percentage": pct,
		"result":     result,
	}).Debug("[router] hash-based routing decision")

	return result
}

// hashTenantBucket maps a tenant ID to an integer in [0, 100).
// Uses FNV-1a 32-bit hash for speed and uniform distribution.
func hashTenantBucket(tenantID string) int {
	h := fnv.New32a()
	_, _ = h.Write([]byte(tenantID))
	return int(h.Sum32() % 100)
}
