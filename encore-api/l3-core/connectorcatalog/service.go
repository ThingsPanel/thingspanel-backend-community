// Package connectorcatalog owns the catalog of connection providers: the signed provider
// releases a device template can be bound to, and the schema each one accepts.
package connectorcatalog

import "github.com/google/uuid"

// Service serves the connection-provider catalog endpoints. It keeps no state of its own;
// the catalog lives in the connector_runtime database.
//
//encore:service
type Service struct{}

func newID() string { return uuid.NewString() }
