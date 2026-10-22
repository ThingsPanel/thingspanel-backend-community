package service

import (
	"context"
	"errors"

	"project/internal/dal"
)

var (
	ErrDefaultPolicyInvalid     = errors.New("notification default policy request invalid")
	ErrDefaultPolicyConflict    = errors.New("notification default policy version conflict")
	ErrDefaultPolicyUnavailable = errors.New("notification default policy unavailable")
)

type DefaultPolicySummary struct {
	NativeGroupID string `json:"nativeGroupId"`
	AliasGroupID  string `json:"aliasGroupId"`
	Name          string `json:"name"`
	GroupRevision int64  `json:"groupRevision"`
	RouteVersion  int64  `json:"routeVersion"`
	Status        string `json:"status"`
	Ready         bool   `json:"ready"`
}

type TenantDefaultPolicyView struct {
	Version           int64                  `json:"version"`
	Selected          *DefaultPolicySummary  `json:"selected"`
	AvailablePolicies []DefaultPolicySummary `json:"availablePolicies"`
}

func GetTenantDefaultPolicy(ctx context.Context, tenantID string) (TenantDefaultPolicyView, error) {
	var view TenantDefaultPolicyView
	bridge := currentSourceBridge()
	if bridge == nil || !bridge.Enabled() || tenantID == "" {
		return view, ErrDefaultPolicyUnavailable
	}
	selected, available, err := dal.GetTenantDefaultPolicy(ctx, bridge.DeploymentID(), tenantID)
	if err != nil {
		return view, ErrDefaultPolicyUnavailable
	}
	view.Version = selected.Version
	view.AvailablePolicies = make([]DefaultPolicySummary, 0, len(available))
	for _, state := range available {
		if !summaryFromNativeState(state).Ready || !defaultPolicySnapshotReady(ctx, bridge, tenantID, state) {
			continue
		}
		view.AvailablePolicies = append(view.AvailablePolicies, summaryFromNativeState(state))
	}
	if selected.NativeGroupID != nil && selected.AliasGroupID != nil {
		nativeID, aliasID := *selected.NativeGroupID, *selected.AliasGroupID
		key := dal.SourceRouteKey{DeploymentID: bridge.DeploymentID(), TenantID: tenantID, LegacyGroup: aliasID}
		state, stateErr := dal.GetNativePublishState(ctx, key, nativeID)
		if stateErr != nil {
			state = dal.NativePublishState{NativeGroupID: nativeID, LegacyGroupID: aliasID, GroupRevision: selected.GroupRevision, Status: "unavailable", Engine: "legacy"}
		} else if summaryFromNativeState(state).Ready && !defaultPolicySnapshotReady(ctx, bridge, tenantID, state) {
			state.Status = "unavailable"
			state.Published = false
		}
		view.Selected = ptrDefaultPolicySummary(summaryFromNativeState(state))
	}
	return view, nil
}

func SetTenantDefaultPolicy(ctx context.Context, tenantID, nativeGroupID string, expectedVersion int64) (TenantDefaultPolicyView, error) {
	var view TenantDefaultPolicyView
	bridge := currentSourceBridge()
	if bridge == nil || !bridge.Enabled() || tenantID == "" || expectedVersion < 0 || !validNativeGroupIDOrEmpty(nativeGroupID) {
		return view, ErrDefaultPolicyInvalid
	}
	aliasID := ""
	if nativeGroupID != "" {
		aliasID = deterministicNativeAliasID(bridge.DeploymentID(), tenantID, nativeGroupID)
		key := dal.SourceRouteKey{DeploymentID: bridge.DeploymentID(), TenantID: tenantID, LegacyGroup: aliasID}
		state, err := dal.GetNativePublishState(ctx, key, nativeGroupID)
		if err != nil {
			return view, ErrDefaultPolicyUnavailable
		}
		if !summaryFromNativeState(state).Ready || !defaultPolicySnapshotReady(ctx, bridge, tenantID, state) {
			return view, ErrDefaultPolicyInvalid
		}
	}
	if _, err := dal.SetTenantDefaultPolicy(ctx, bridge.DeploymentID(), tenantID, nativeGroupID, aliasID, expectedVersion); err != nil {
		switch {
		case errors.Is(err, dal.ErrDefaultPolicyConflict):
			return view, ErrDefaultPolicyConflict
		case errors.Is(err, dal.ErrDefaultPolicyInvalid):
			return view, ErrDefaultPolicyInvalid
		default:
			return view, ErrDefaultPolicyUnavailable
		}
	}
	return GetTenantDefaultPolicy(ctx, tenantID)
}

// defaultPolicySnapshotReady reuses the same bounded, TLS-verified Core
// snapshot check used before publishing. It runs only in management reads and
// writes, never while an alarm transaction holds its routing locks.
func defaultPolicySnapshotReady(ctx context.Context, bridge *SourceBridge, tenantID string, state dal.NativePublishState) bool {
	if bridge == nil || !bridge.Enabled() || tenantID == "" || !state.Published || !state.Enabled || state.Engine != "encore" || state.LegacyGroupID == "" || state.NativeGroupID == "" || state.GroupRevision < 1 {
		return false
	}
	request := SourceGroupProjectionRequest{
		SourceDeploymentID: bridge.DeploymentID(), TenantID: tenantID,
		LegacyGroupID: state.LegacyGroupID, NotificationGroupID: state.NativeGroupID,
		GroupRevision: state.GroupRevision,
	}
	return bridge.ValidateNativePublishSnapshot(ctx, request) == nil
}

func summaryFromNativeState(state dal.NativePublishState) DefaultPolicySummary {
	return DefaultPolicySummary{NativeGroupID: state.NativeGroupID, AliasGroupID: state.LegacyGroupID, Name: state.Name,
		GroupRevision: state.GroupRevision, RouteVersion: state.RouteVersion, Status: state.Status, Ready: state.Published && state.Enabled && state.Engine == "encore"}
}

func ptrDefaultPolicySummary(value DefaultPolicySummary) *DefaultPolicySummary { return &value }

func validNativeGroupIDOrEmpty(value string) bool {
	if value == "" {
		return true
	}
	return validNativeGroupID(value)
}
