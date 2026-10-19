package service

import "project/pkg/errcode"

// Legacy-only product families stay unavailable on the community ingress
// profile. Supported standard controls use the durable owner instead.
func requireLegacyDownlink(kind string) error {
	if isIngressV1Commands() {
		return errcode.NewWithMessage(errcode.CodeParamError, "unsupported_capability: "+kind+" is not supported by the community ingress_v1 profile")
	}
	return nil
}
