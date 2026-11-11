-- Hand-written MySQL twin of the plugin_install_events part of sql/34.sql.
CREATE TABLE IF NOT EXISTS `plugin_install_events` (
 `id` bigint NOT NULL AUTO_INCREMENT,
 `installation_id` varchar(36) NOT NULL,
 `op_id` varchar(36) NOT NULL,
 `step` varchar(32) NOT NULL,
 `outcome` varchar(16) NOT NULL,
 `code` varchar(48) NOT NULL DEFAULT '',
 `created_at` datetime(6) NOT NULL,
 PRIMARY KEY (`id`),
 KEY `idx_plugin_install_events_inst` (`installation_id`, `id`)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_bin
