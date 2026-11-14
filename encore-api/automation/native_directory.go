package automation

import (
	"context"
	"encoding/json"
	"errors"
	"sort"
	"strings"

	"encore.app/automation/ports"
	"encore.app/devicemetadata"
	"encore.app/internal/tenantidentity"
)

// The metadata owner is authoritative for template membership. Runtime shadow
// rows in Gin do not carry native template bindings and cannot serve this port.
type nativeDirectory struct{}

func (nativeDirectory) resolve(ctx context.Context, tenant string, req *devicemetadata.ResolveAutomationDirectoryRequest) ([]ports.Device, error) {
	req.TenantID = tenant
	response, err := devicemetadata.ResolveAutomationDirectory(ctx, req)
	if err != nil {
		return nil, err
	}
	return nativeDirectoryDevices(tenant, response)
}

func nativeDirectoryDevices(tenant string, response *devicemetadata.ResolveAutomationDirectoryResponse) ([]ports.Device, error) {
	if response == nil || !response.Complete {
		return nil, errors.New("native device directory returned an incomplete result")
	}
	out := make([]ports.Device, 0, len(response.Devices))
	for _, d := range response.Devices {
		if d.TenantID != tenantidentity.NormalizePrincipalID(tenant) {
			return nil, errors.New("native device directory returned a device outside requested tenant")
		}
		labels := map[string]string{}
		if len(d.Labels) > 0 && json.Unmarshal(d.Labels, &labels) != nil {
			return nil, errors.New("native device directory returned invalid labels")
		}
		tokens := make([]string, 0, len(labels))
		for key, value := range labels {
			tokens = append(tokens, key+"="+value)
		}
		sort.Strings(tokens)
		device := ports.Device{ID: d.ID, TenantID: tenant, Name: d.Name, DeviceType: d.DeviceKind, Label: strings.Join(tokens, ",")}
		if d.TemplateID != nil {
			device.DeviceConfigID = *d.TemplateID
		}
		out = append(out, device)
	}
	return out, nil
}

func (d nativeDirectory) ByIDs(ctx context.Context, tenant string, ids []string) ([]ports.Device, error) {
	return d.resolve(ctx, tenant, &devicemetadata.ResolveAutomationDirectoryRequest{Selector: "ids", IDs: ids})
}
func (d nativeDirectory) ByDeviceConfig(ctx context.Context, tenant string, ids []string) ([]ports.Device, error) {
	return d.resolve(ctx, tenant, &devicemetadata.ResolveAutomationDirectoryRequest{Selector: "templates", IDs: ids})
}
func (d nativeDirectory) ByLabels(ctx context.Context, tenant string, labels []string, all bool) ([]ports.Device, error) {
	return d.resolve(ctx, tenant, &devicemetadata.ResolveAutomationDirectoryRequest{Selector: "labels", Labels: labels, MatchAll: all})
}
func (d nativeDirectory) ByType(ctx context.Context, tenant, kind string) ([]ports.Device, error) {
	return d.resolve(ctx, tenant, &devicemetadata.ResolveAutomationDirectoryRequest{Selector: "kind", DeviceKind: kind})
}
