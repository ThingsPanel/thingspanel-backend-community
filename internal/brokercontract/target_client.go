package brokercontract

import (
	"encoding/base64"
	"errors"
	"strings"
)

const targetClientSeparator = "--target-v1--"

// TargetPublisherClientID selects one target for an explicitly enabled service.
// It is not a credential: the owner still authenticates the service and resolves
// the current target and tenant before granting any concrete publish topics.
func TargetPublisherClientID(base, target string) (string, error) {
	if !validIdentity(base) || strings.Contains(base, targetClientSeparator) || !validIdentity(target) || strings.TrimSpace(target) != target || strings.ContainsAny(target, "+#/\\") {
		return "", errors.New("invalid target publisher identity")
	}
	id := base + targetClientSeparator + base64.RawURLEncoding.EncodeToString([]byte(target))
	if !validIdentity(id) {
		return "", errors.New("target publisher client id exceeds limit")
	}
	return id, nil
}

func ParseTargetPublisherClientID(base, clientID string) (string, bool) {
	prefix := base + targetClientSeparator
	if !strings.HasPrefix(clientID, prefix) {
		return "", false
	}
	decoded, err := base64.RawURLEncoding.DecodeString(strings.TrimPrefix(clientID, prefix))
	if err != nil {
		return "", false
	}
	target := string(decoded)
	canonical, err := TargetPublisherClientID(base, target)
	return target, err == nil && canonical == clientID
}
