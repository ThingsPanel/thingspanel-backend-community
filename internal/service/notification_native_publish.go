package service

import (
	"context"
	"crypto/sha1"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"
	"unicode"

	"project/internal/dal"
)

var (
	ErrNativePublishInvalid     = errors.New("native notification publish request invalid")
	ErrNativePublishConflict    = errors.New("native notification publish route changed")
	ErrNativePublishUnavailable = errors.New("native notification publish unavailable")
)

type NativePublishStatus struct {
	Published              bool   `json:"published"`
	Status                 string `json:"status"`
	LegacyGroupID          string `json:"legacyGroupId"`
	NativeGroupID          string `json:"nativeGroupId"`
	GroupRevision          int64  `json:"groupRevision"`
	EffectiveGroupRevision int64  `json:"effectiveGroupRevision"`
	RouteVersion           int64  `json:"routeVersion"`
	Engine                 string `json:"engine"`
	Enabled                bool   `json:"enabled"`
	Name                   string `json:"name"`
}

type NativePublishRequest struct {
	Operation            string
	TenantID             string
	NativeGroupID        string
	GroupRevision        int64
	Name                 string
	ExpectedRouteVersion int64
	IdempotencyKey       string
}

func GetNativePublishStatus(ctx context.Context, tenantID, nativeGroupID string) (NativePublishStatus, error) {
	var empty NativePublishStatus
	bridge := currentSourceBridge()
	if bridge == nil || !bridge.Enabled() || tenantID == "" || !validNativeGroupID(nativeGroupID) {
		return empty, ErrNativePublishUnavailable
	}
	key := nativePublishRouteKey(bridge.DeploymentID(), tenantID, nativeGroupID)
	state, err := dal.GetNativePublishState(ctx, key, nativeGroupID)
	if err != nil {
		return empty, mapNativePublishStoreError(err)
	}
	return toNativePublishStatus(state), nil
}

func ApplyNativePublish(ctx context.Context, request NativePublishRequest) (NativePublishStatus, bool, error) {
	var empty NativePublishStatus
	if request.TenantID == "" || !validNativeGroupID(request.NativeGroupID) || request.ExpectedRouteVersion < 0 || !validNativePublishIdempotencyKey(request.IdempotencyKey) {
		return empty, false, ErrNativePublishInvalid
	}
	operation := request.Operation
	if operation == "" {
		operation = "publish"
	}
	if operation != "publish" && operation != "unpublish" {
		return empty, false, ErrNativePublishInvalid
	}
	bridge := currentSourceBridge()
	if bridge == nil || !bridge.Enabled() || bridge.DeploymentID() == "" {
		return empty, false, ErrNativePublishUnavailable
	}
	key := nativePublishRouteKey(bridge.DeploymentID(), request.TenantID, request.NativeGroupID)

	if operation == "unpublish" {
		state, replayed, err := dal.StopNativePublishRoute(ctx, key, request.NativeGroupID, request.ExpectedRouteVersion)
		if err != nil {
			return empty, false, mapNativePublishStoreError(err)
		}
		return toNativePublishStatus(state), replayed, nil
	}
	if request.GroupRevision < 1 || !validNativePublishName(request.Name) {
		return empty, false, ErrNativePublishInvalid
	}
	current, err := dal.GetNativePublishState(ctx, key, request.NativeGroupID)
	if err != nil {
		return empty, false, mapNativePublishStoreError(err)
	}
	if current.Published {
		if current.GroupRevision == request.GroupRevision && current.Name == request.Name {
			return toNativePublishStatus(current), true, nil
		}
		if current.RouteVersion != request.ExpectedRouteVersion {
			return empty, false, ErrNativePublishConflict
		}
	}
	if current.RouteVersion != request.ExpectedRouteVersion {
		return empty, false, ErrNativePublishConflict
	}
	projection := SourceGroupProjectionRequest{
		SourceDeploymentID: key.DeploymentID, TenantID: key.TenantID,
		LegacyGroupID: key.LegacyGroup, NotificationGroupID: request.NativeGroupID,
		GroupRevision: request.GroupRevision,
	}
	if err := bridge.ValidateNativePublishSnapshot(ctx, projection); err != nil {
		return empty, false, ErrNativePublishUnavailable
	}
	if err := dal.EnsureNativePublishAlias(ctx, key, key.LegacyGroup, request.Name); err != nil {
		return empty, false, mapNativePublishStoreError(err)
	}
	if _, err := bridge.RegisterProjection(ctx, projection, request.IdempotencyKey); err != nil {
		return empty, false, ErrNativePublishUnavailable
	}
	state, replayed, err := dal.ActivateNativePublishRoute(ctx, key, request.NativeGroupID, request.Name,
		request.GroupRevision, request.ExpectedRouteVersion, request.IdempotencyKey)
	if err != nil {
		return empty, false, mapNativePublishStoreError(err)
	}
	return toNativePublishStatus(state), replayed, nil
}

// ValidateNativePublishSnapshot checks the exact tenant-scoped Core revision
// and the enabled instance/plugin rows before a community alias can open. The
// Core snapshot endpoint applies the same tenant-grant scope when assembling
// its plugin registry; every referenced registration must be present here.
func (s *SourceBridge) ValidateNativePublishSnapshot(ctx context.Context, request SourceGroupProjectionRequest) error {
	if !s.Enabled() || s.base == nil || request.SourceDeploymentID != s.config.DeploymentID || request.TenantID == "" || request.NotificationGroupID == "" || request.GroupRevision < 1 {
		return errors.New("native publish snapshot unavailable")
	}
	snapshot, err := fetchSourceGroupSnapshot(ctx, s.client, s.base, s.config.ProjectionBearerToken, request)
	if err != nil || snapshot.SourceDeploymentID != request.SourceDeploymentID || snapshot.TenantID != request.TenantID ||
		snapshot.Group.ID != request.NotificationGroupID || !snapshot.Group.Enabled || snapshot.Group.Revision != request.GroupRevision {
		return errors.New("native publish snapshot unavailable")
	}
	instances := make(map[string]sourceSnapshotInstance, len(snapshot.Instances))
	for _, instance := range snapshot.Instances {
		if instance.ID == "" || instance.PluginRegistrationID == "" || !instance.Enabled || instance.ConfigVersion < 1 {
			return errors.New("native publish target unavailable")
		}
		if _, duplicate := instances[instance.ID]; duplicate {
			return errors.New("native publish target unavailable")
		}
		instances[instance.ID] = instance
	}
	plugins := make(map[string]sourceSnapshotPlugin, len(snapshot.Plugins))
	for _, plugin := range snapshot.Plugins {
		if plugin.ID == "" || plugin.PluginID == "" || plugin.PluginVersion == "" || !plugin.Enabled {
			return errors.New("native publish plugin unavailable")
		}
		if _, duplicate := plugins[plugin.ID]; duplicate {
			return errors.New("native publish plugin unavailable")
		}
		plugins[plugin.ID] = plugin
	}
	if len(snapshot.Group.Bindings) == 0 {
		return errors.New("native publish group has no targets")
	}
	usedInstances := make(map[string]struct{}, len(snapshot.Group.Bindings))
	usedPlugins := make(map[string]struct{}, len(snapshot.Group.Bindings))
	for _, binding := range snapshot.Group.Bindings {
		instance, ok := instances[binding.InstanceID]
		if !ok || binding.BindingID == "" {
			return errors.New("native publish binding unavailable")
		}
		_, ok = plugins[instance.PluginRegistrationID]
		if !ok {
			return errors.New("native publish grant unavailable")
		}
		usedInstances[instance.ID] = struct{}{}
		usedPlugins[instance.PluginRegistrationID] = struct{}{}
	}
	if len(usedInstances) != len(instances) || len(usedPlugins) != len(plugins) {
		return errors.New("native publish snapshot has unbound targets")
	}
	return nil
}

func nativePublishRouteKey(deploymentID, tenantID, nativeGroupID string) dal.SourceRouteKey {
	return dal.SourceRouteKey{DeploymentID: deploymentID, TenantID: tenantID, LegacyGroup: deterministicNativeAliasID(deploymentID, tenantID, nativeGroupID)}
}

func deterministicNativeAliasID(deploymentID, tenantID, nativeGroupID string) string {
	// UUIDv5 with a fixed namespace and length-prefixed fields gives each
	// deployment/tenant/native-group tuple one stable legacy selector ID.
	namespace, _ := hex.DecodeString("2a6e7a98b3b54e3bb34af6dc79f99a01")
	name := fmt.Sprintf("%d:%s%d:%s%d:%s", len(deploymentID), deploymentID, len(tenantID), tenantID, len(nativeGroupID), nativeGroupID)
	input := append(append([]byte(nil), namespace...), []byte(name)...)
	digest := sha1.Sum(input)
	id := digest[:16]
	id[6] = (id[6] & 0x0f) | 0x50
	id[8] = (id[8] & 0x3f) | 0x80
	encoded := hex.EncodeToString(id)
	return encoded[:8] + "-" + encoded[8:12] + "-" + encoded[12:16] + "-" + encoded[16:20] + "-" + encoded[20:]
}

func validNativePublishIdempotencyKey(key string) bool {
	if len(key) < 8 || len(key) > 128 || strings.TrimSpace(key) != key {
		return false
	}
	for _, r := range key {
		if unicode.IsSpace(r) || unicode.IsControl(r) {
			return false
		}
	}
	return true
}

func validNativeGroupID(id string) bool {
	if id == "" || len(id) > 128 || strings.TrimSpace(id) != id {
		return false
	}
	for _, r := range id {
		if unicode.IsSpace(r) || unicode.IsControl(r) {
			return false
		}
	}
	return true
}

func validNativePublishName(name string) bool {
	if strings.TrimSpace(name) == "" || len([]rune(name)) > 128 || strings.TrimSpace(name) != name {
		return false
	}
	for _, r := range name {
		if unicode.IsControl(r) {
			return false
		}
	}
	return true
}

func mapNativePublishStoreError(err error) error {
	switch {
	case errors.Is(err, dal.ErrSourceRouteConflict), errors.Is(err, dal.ErrNotificationGroupReadOnly):
		return ErrNativePublishConflict
	case errors.Is(err, dal.ErrSourceRouteUnavailable):
		return ErrNativePublishUnavailable
	default:
		return ErrNativePublishUnavailable
	}
}

func toNativePublishStatus(state dal.NativePublishState) NativePublishStatus {
	return NativePublishStatus{
		Published: state.Published, Status: state.Status, LegacyGroupID: state.LegacyGroupID,
		NativeGroupID: state.NativeGroupID, GroupRevision: state.GroupRevision,
		EffectiveGroupRevision: state.EffectiveGroupRevision, RouteVersion: state.RouteVersion,
		Engine: state.Engine, Enabled: state.Enabled, Name: state.Name,
	}
}
