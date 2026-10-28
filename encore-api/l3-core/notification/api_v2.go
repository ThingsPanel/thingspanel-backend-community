package notification

import "net/http"

// Auth is checked by the notification principal verifier on each raw route.
// User sessions, source identities and receipt identities have separate scopes.

//encore:api public raw method=GET,POST path=/api/v2/notification-plugins
func (s *Service) PluginsV2(w http.ResponseWriter, r *http.Request) { s.api.ServeHTTP(w, r) }

//encore:api public raw method=GET,PUT path=/api/v2/notification-plugins/:id
func (s *Service) PluginV2(w http.ResponseWriter, r *http.Request) { s.api.ServeHTTP(w, r) }

//encore:api public raw method=GET,POST path=/api/v2/notification-instances
func (s *Service) InstancesV2(w http.ResponseWriter, r *http.Request) { s.api.ServeHTTP(w, r) }

// The frozen URLs share one native raw family because Encore's route trie
// rejects POST /validate beside POST /:id/test. The handler keeps the exact
// OpenAPI paths, methods and independent permission checks.
//
//encore:api public raw method=GET,POST,PUT path=/api/v2/notification-instances/*path
func (s *Service) InstanceRoutesV2(w http.ResponseWriter, r *http.Request) { s.api.ServeHTTP(w, r) }

// Platform account operations require SYS_ADMIN and an explicit tenant path.
//
//encore:api public raw method=GET,POST,PUT path=/api/v2/platform/notification-tenants/*path
func (s *Service) PlatformTenantInstanceRoutesV2(w http.ResponseWriter, r *http.Request) {
	s.api.ServeHTTP(w, r)
}

//encore:api public raw method=GET,POST path=/api/v2/notifications
func (s *Service) SubmitV2(w http.ResponseWriter, r *http.Request) { s.api.ServeHTTP(w, r) }

//encore:api public raw method=GET path=/api/v2/notifications/:id
func (s *Service) NotificationV2(w http.ResponseWriter, r *http.Request) { s.api.ServeHTTP(w, r) }

//encore:api public raw method=GET path=/api/v2/notification-deliveries
func (s *Service) DeliveriesV2(w http.ResponseWriter, r *http.Request) { s.api.ServeHTTP(w, r) }

//encore:api public raw method=GET path=/api/v2/notification-deliveries/:id
func (s *Service) DeliveryV2(w http.ResponseWriter, r *http.Request) { s.api.ServeHTTP(w, r) }

//encore:api public raw method=POST path=/api/v2/notification-ingress/events
func (s *Service) SourceEventV2(w http.ResponseWriter, r *http.Request) { s.api.ServeHTTP(w, r) }

//encore:api public raw method=POST path=/api/v1/notification-instances/:instance_id/receipts
func (s *Service) ReceiptV1(w http.ResponseWriter, r *http.Request) { s.api.ServeHTTP(w, r) }

//encore:api public raw method=GET,POST path=/api/v2/notification-groups
func (s *Service) GroupsV2(w http.ResponseWriter, r *http.Request) { s.api.ServeHTTP(w, r) }

//encore:api public raw method=GET,PUT path=/api/v2/notification-groups/:id
func (s *Service) GroupV2(w http.ResponseWriter, r *http.Request) { s.api.ServeHTTP(w, r) }

//encore:api public raw method=POST path=/api/v2/notification-group-projections
func (s *Service) GroupProjectionsV2(w http.ResponseWriter, r *http.Request) { s.api.ServeHTTP(w, r) }

//encore:api public raw method=GET,PUT path=/api/v2/notification-plugin-grants
func (s *Service) PluginGrantsV2(w http.ResponseWriter, r *http.Request) { s.api.ServeHTTP(w, r) }

// This deployment adapter requires the separate projection identity.
//
//encore:api public raw method=GET path=/api/v1/notification-source/group-snapshots/:id
func (s *Service) SourceGroupSnapshotV1(w http.ResponseWriter, r *http.Request) {
	s.api.ServeHTTP(w, r)
}
