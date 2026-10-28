package notification

import (
	"encoding/json"
	"net/http"
	"sort"
	"strconv"
	"strings"

	"encore.app/l3-core/notification/contract"
)

// This private deployment adapter exposes only the safe fields needed to
// compare a legacy group with a published native revision before source CAS.
type sourceGroupInspection struct {
	SourceDeploymentID string                     `json:"sourceDeploymentId"`
	TenantID           string                     `json:"tenantId"`
	Group              sourceInspectionGroup      `json:"group"`
	Instances          []sourceInspectionInstance `json:"instances"`
	Plugins            []sourceInspectionPlugin   `json:"plugins"`
}
type sourceInspectionGroup struct {
	ID       string                  `json:"id"`
	Enabled  bool                    `json:"enabled"`
	Revision int64                   `json:"revision"`
	Bindings []contract.GroupBinding `json:"bindings"`
}
type sourceInspectionInstance struct {
	ID                   string                     `json:"id"`
	PluginRegistrationID string                     `json:"pluginRegistrationId"`
	Channel              contract.Channel           `json:"channel"`
	Enabled              bool                       `json:"enabled"`
	Config               map[string]json.RawMessage `json:"config"`
	SecretState          map[string]bool            `json:"secretState"`
	ProviderIdentity     map[string]string          `json:"providerIdentity"`
	ConfigVersion        int64                      `json:"configVersion"`
}
type sourceInspectionPlugin struct {
	ID            string `json:"id"`
	PluginID      string `json:"pluginId"`
	PluginVersion string `json:"pluginVersion"`
	Enabled       bool   `json:"enabled"`
}

func (h *NotificationHTTPHandler) serveSourceGroupInspection(w http.ResponseWriter, r *http.Request) bool {
	const prefix = "/api/v1/notification-source/group-snapshots/"
	if !strings.HasPrefix(r.URL.Path, prefix) {
		return false
	}
	principal, err := h.Verifier.VerifySourceProjection(r.Context(), r.Header.Get("Authorization"))
	if err != nil {
		writeNotificationFailure(w, r, coreFailure(401, "source_authentication_required"))
		return true
	}
	id, err := managementPathID(r.URL.Path, prefix)
	query := r.URL.Query()
	valid := r.Method == http.MethodGet && r.ContentLength == 0 && len(query) == 2 && len(query["tenantId"]) == 1 && len(query["groupRevision"]) == 1
	revision, parseErr := strconv.ParseInt(query.Get("groupRevision"), 10, 64)
	if err != nil || !valid || parseErr != nil || revision < 1 || !validDirectoryPart(query.Get("tenantId")) {
		writeNotificationFailure(w, r, coreFailure(400, "invalid_query"))
		return true
	}
	tenant := query.Get("tenantId")
	if principal.DeploymentID == "" || !containsString(principal.TenantIDs, tenant) {
		writeNotificationFailure(w, r, coreFailure(403, "source_scope_mismatch"))
		return true
	}
	if h.Store == nil {
		writeNotificationFailure(w, r, coreFailure(503, "notification_unavailable"))
		return true
	}
	group, err := h.Store.GetGroupRevision(r.Context(), tenant, id, revision)
	if err != nil {
		writeNotificationFailure(w, r, err)
		return true
	}
	if !group.Enabled {
		writeNotificationFailure(w, r, coreFailure(404, "not_found"))
		return true
	}
	result := sourceGroupInspection{SourceDeploymentID: principal.DeploymentID, TenantID: tenant, Group: sourceInspectionGroup{ID: group.ID, Enabled: group.Enabled, Revision: group.Revision, Bindings: group.Bindings}, Instances: []sourceInspectionInstance{}, Plugins: []sourceInspectionPlugin{}}
	instances, plugins := map[string]bool{}, map[string]bool{}
	for _, binding := range group.Bindings {
		if instances[binding.InstanceID] {
			continue
		}
		instance, err := h.Store.GetInstance(r.Context(), tenant, binding.InstanceID)
		if err != nil {
			writeNotificationFailure(w, r, err)
			return true
		}
		if !instance.Enabled {
			writeNotificationFailure(w, r, coreFailure(404, "not_found"))
			return true
		}
		instances[instance.ID] = true
		result.Instances = append(result.Instances, sourceInspectionInstance{instance.ID, instance.PluginRegistrationID, instance.Channel, instance.Enabled, instance.Config, instance.SecretState, instance.ProviderIdentity, instance.ConfigVersion})
		if plugins[instance.PluginRegistrationID] {
			continue
		}
		plugin, err := h.Store.GetPlugin(r.Context(), tenant, instance.PluginRegistrationID)
		if err != nil {
			writeNotificationFailure(w, r, err)
			return true
		}
		if !plugin.Enabled {
			writeNotificationFailure(w, r, coreFailure(404, "not_found"))
			return true
		}
		plugins[plugin.ID] = true
		result.Plugins = append(result.Plugins, sourceInspectionPlugin{plugin.ID, plugin.PluginID, plugin.PluginVersion, plugin.Enabled})
	}
	sort.Slice(result.Instances, func(i, j int) bool { return result.Instances[i].ID < result.Instances[j].ID })
	sort.Slice(result.Plugins, func(i, j int) bool { return result.Plugins[i].ID < result.Plugins[j].ID })
	writeManagementSuccess(w, r, http.StatusOK, result)
	return true
}
