-- Hand-written MySQL twin of sql/33.sql (managed plugin runtime identities). Timestamps are UTC, set by the application.
CREATE TABLE IF NOT EXISTS `plugin_runtime_identities` (
 `instance_id` varchar(64) NOT NULL,
 `plugin_instance` varchar(128) NOT NULL,
 `service_identifier` varchar(100) NOT NULL,
 `tenant_id` varchar(36) NOT NULL,
 `generation` bigint NOT NULL,
 `secret_sha256` char(64) NOT NULL,
 `created_at` datetime(6) NOT NULL,
 `expires_at` datetime(6) NOT NULL,
 `revoked_at` datetime(6) NULL,
 PRIMARY KEY (`instance_id`),
 UNIQUE KEY `uq_plugin_runtime_identity_generation` (`tenant_id`, `plugin_instance`, `generation`),
 CONSTRAINT `chk_plugin_runtime_identity_generation` CHECK (`generation` >= 1)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_bin
