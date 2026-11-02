package brokercontract

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"sort"
)

func PluginContextID(principal Principal, cell, instance, connectionID string, targets []PluginTarget) (string, error) {
	targets = append([]PluginTarget(nil), targets...)
	sort.Slice(targets, func(i, j int) bool {
		if targets[i].TenantID != targets[j].TenantID {
			return targets[i].TenantID < targets[j].TenantID
		}
		return targets[i].DeviceID < targets[j].DeviceID
	})
	data, err := json.Marshal(struct {
		Principal  Principal      `json:"principal"`
		Cell       string         `json:"cell"`
		Instance   string         `json:"instance"`
		Connection string         `json:"connection"`
		Targets    []PluginTarget `json:"targets"`
	}{principal, cell, instance, connectionID, targets})
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:]), nil
}
