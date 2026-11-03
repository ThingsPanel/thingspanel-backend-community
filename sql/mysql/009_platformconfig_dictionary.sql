-- MySQL form of the platform dictionary (sys_dict, sys_dict_language) with the seed rows that sql/1.sql
-- inserts. Column lists, uniqueness and the cascade follow the effective PostgreSQL schema.
-- dict_code/dict_value/language_code stay case-sensitive (utf8mb4_bin) like PostgreSQL's comparison.
-- @@
CREATE TABLE IF NOT EXISTS sys_dict (
    id VARCHAR(36) CHARACTER SET utf8mb4 COLLATE utf8mb4_bin NOT NULL,
    dict_code VARCHAR(36) CHARACTER SET utf8mb4 COLLATE utf8mb4_bin NOT NULL,
    dict_value VARCHAR(255) CHARACTER SET utf8mb4 COLLATE utf8mb4_bin NOT NULL,
    created_at DATETIME(6) NOT NULL,
    remark VARCHAR(255) NULL,
    PRIMARY KEY (id),
    CONSTRAINT sys_dict_dict_code_dict_value_key UNIQUE (dict_code, dict_value)
) ENGINE = InnoDB DEFAULT CHARSET = utf8mb4
-- @@
CREATE TABLE IF NOT EXISTS sys_dict_language (
    id VARCHAR(36) CHARACTER SET utf8mb4 COLLATE utf8mb4_bin NOT NULL,
    dict_id VARCHAR(36) CHARACTER SET utf8mb4 COLLATE utf8mb4_bin NOT NULL,
    language_code VARCHAR(36) CHARACTER SET utf8mb4 COLLATE utf8mb4_bin NOT NULL,
    translation VARCHAR(255) NOT NULL,
    PRIMARY KEY (id),
    CONSTRAINT sys_dict_language_dict_id_language_code_key UNIQUE (dict_id, language_code),
    CONSTRAINT sys_dict_language_dict_id_fkey FOREIGN KEY (dict_id) REFERENCES sys_dict (id) ON DELETE CASCADE
) ENGINE = InnoDB DEFAULT CHARSET = utf8mb4
-- @@
INSERT IGNORE INTO sys_dict (id, dict_code, dict_value, created_at, remark) VALUES
    ('0013fb9e-e3be-95d4-9c96-f18d1f9ddfcd', 'GATEWAY_PROTOCOL', 'MQTT', '2024-01-18 15:39:38.469000', NULL),
    ('7162fb9e-e3be-95d4-9c96-f18d1f9ddfcd', 'DRIECT_ATTACHED_PROTOCOL', 'MQTT', '2024-01-18 15:39:38.469000', NULL)
-- @@
INSERT IGNORE INTO sys_dict_language (id, dict_id, language_code, translation) VALUES
    ('001c3960-3067-536d-5c97-7645351a687c', '7162fb9e-e3be-95d4-9c96-f18d1f9ddfcd', 'zh_CN', 'MQTT协议'),
    ('002c3960-3067-536d-5c97-7645351a687b', '0013fb9e-e3be-95d4-9c96-f18d1f9ddfcd', 'zh_CN', 'MQTT协议(网关)'),
    ('7162fb9e-e3be-95d4-9c96-f18d1f9ddfff', '0013fb9e-e3be-95d4-9c96-f18d1f9ddfcd', 'en_US', 'MQTT Protocol(Gateway)'),
    ('7162fb9e-e3be-95d4-9c96-f18d1f9ddfss', '7162fb9e-e3be-95d4-9c96-f18d1f9ddfcd', 'en_US', 'MQTT Protocol')
