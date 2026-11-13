package global

import (
	"os"
	"regexp"
	"strings"
	"sync"

	"github.com/sirupsen/logrus"
)

// VersionOverrideEnv lets an operator re-label an already built image at run
// time (e.g. promote v1.4.0-beta.2 to v1.4.0 without rebuilding). It changes
// only the reported version string; the DB schema ledger (sys_version /
// VERSION_NUMBER) and migrations are never affected.
const VersionOverrideEnv = "TP_VERSION_OVERRIDE"

var versionOverridePattern = regexp.MustCompile(`^v?\d+\.\d+\.\d+(-beta\.\d+)?$`)

// resolveVersion returns the override when it is set and valid, otherwise the
// compiled version. warn is true when an override was set but rejected.
func resolveVersion(override, compiled string) (version string, warn bool) {
	override = strings.TrimSpace(override)
	if override == "" {
		return compiled, false
	}
	if versionOverridePattern.MatchString(override) {
		return override, false
	}
	return compiled, true
}

var (
	versionOnce   sync.Once
	versionCached string
)

// SystemVersion is the platform version to report: TP_VERSION_OVERRIDE when
// valid, else the ldflags-injected SYSTEM_VERSION. Resolved once per process.
func SystemVersion() string {
	versionOnce.Do(func() {
		raw := os.Getenv(VersionOverrideEnv)
		v, warn := resolveVersion(raw, SYSTEM_VERSION)
		if warn {
			logrus.Warnf("%s=%q is not a valid version (want vX.Y.Z or vX.Y.Z-beta.N); ignoring it and reporting %q", VersionOverrideEnv, raw, v)
		}
		versionCached = v
	})
	return versionCached
}
