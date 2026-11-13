package dal

// Mainline integration retains the existing device/configuration lock order,
// role-reference checks, binding CAS and unknown-commit handling.
func updateDeviceConfigMetadataOwner(id, tenantID string, values map[string]interface{}, revision *int, result *DeviceConfigMutation) error {
	return updateDeviceConfigForOwner(id, tenantID, values, revision, result)
}
