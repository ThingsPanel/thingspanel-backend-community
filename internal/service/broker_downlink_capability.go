package service

import "project/pkg/errcode"

// Until these wire families have the same durable intent/fence/receipt owner,
// accepting their old in-memory enqueue would misrepresent successful control.
func requireLegacyDownlink(kind string) error {
	if isIngressV1Commands() {
		return errcode.NewWithMessage(errcode.CodeParamError, "unsupported_capability: "+kind+" has no certified durable ingress_v1 downlink")
	}
	return nil
}
