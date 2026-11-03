-- Tables the account flows need besides users: boards (the home board created at sign-up), sys_function
-- (feature flags), user_address (profile address) and casbin_rule (role assignments; the gorm adapter
-- creates the same table when it starts, CREATE IF NOT EXISTS lets both coexist).
-- Column lists follow the effective PostgreSQL schema after sql/1.sql..32.sql.
-- @@
CREATE TABLE IF NOT EXISTS boards (
    id VARCHAR(36) NOT NULL,
    name VARCHAR(255) NOT NULL,
    config JSON NULL DEFAULT (JSON_OBJECT()),
    tenant_id VARCHAR(36) NOT NULL,
    created_at DATETIME(6) NOT NULL,
    updated_at DATETIME(6) NOT NULL,
    home_flag VARCHAR(2) NOT NULL,
    description VARCHAR(500) NULL,
    remark VARCHAR(255) NULL,
    menu_flag VARCHAR(2) NULL,
    vis_type VARCHAR(50) NULL,
    PRIMARY KEY (id),
    KEY idx_boards_tenant (tenant_id)
) ENGINE = InnoDB DEFAULT CHARSET = utf8mb4
-- @@
CREATE TABLE IF NOT EXISTS sys_function (
    id VARCHAR(36) NOT NULL,
    name VARCHAR(50) NOT NULL,
    enable_flag VARCHAR(20) NOT NULL,
    description VARCHAR(500) NULL,
    remark VARCHAR(255) NULL,
    PRIMARY KEY (id)
) ENGINE = InnoDB DEFAULT CHARSET = utf8mb4
-- @@
INSERT IGNORE INTO sys_function (id, name, enable_flag, description) VALUES
    ('function_1', 'use_captcha', 'disable', '验证码登陆'),
    ('function_2', 'enable_reg', 'disable', '租户注册'),
    ('function_3', 'frontend_res', 'disable', '前端RSA加密'),
    ('function_4', 'shared_account', 'enable', '共享账号')
-- @@
CREATE TABLE IF NOT EXISTS user_address (
    id INT NOT NULL AUTO_INCREMENT,
    user_id VARCHAR(36) NOT NULL,
    country VARCHAR(50) NULL,
    province VARCHAR(50) NULL,
    city VARCHAR(50) NULL,
    district VARCHAR(50) NULL,
    street VARCHAR(100) NULL,
    detailed_address VARCHAR(200) NULL,
    postal_code VARCHAR(10) NULL,
    address_label VARCHAR(50) NULL,
    longitude VARCHAR(20) NULL,
    latitude VARCHAR(20) NULL,
    additional_info VARCHAR(500) NULL,
    created_time DATETIME(6) NULL DEFAULT CURRENT_TIMESTAMP(6),
    updated_time DATETIME(6) NULL DEFAULT CURRENT_TIMESTAMP(6),
    PRIMARY KEY (id),
    UNIQUE KEY uk_user_address_user_id (user_id),
    CONSTRAINT fk_user_address_user_id FOREIGN KEY (user_id) REFERENCES users (id) ON DELETE CASCADE
) ENGINE = InnoDB DEFAULT CHARSET = utf8mb4
-- @@
CREATE TABLE IF NOT EXISTS casbin_rule (
    id BIGINT UNSIGNED NOT NULL AUTO_INCREMENT,
    ptype VARCHAR(100) CHARACTER SET utf8mb4 COLLATE utf8mb4_bin NULL,
    v0 VARCHAR(100) CHARACTER SET utf8mb4 COLLATE utf8mb4_bin NULL,
    v1 VARCHAR(100) CHARACTER SET utf8mb4 COLLATE utf8mb4_bin NULL,
    v2 VARCHAR(100) CHARACTER SET utf8mb4 COLLATE utf8mb4_bin NULL,
    v3 VARCHAR(100) CHARACTER SET utf8mb4 COLLATE utf8mb4_bin NULL,
    v4 VARCHAR(100) CHARACTER SET utf8mb4 COLLATE utf8mb4_bin NULL,
    v5 VARCHAR(100) CHARACTER SET utf8mb4 COLLATE utf8mb4_bin NULL,
    PRIMARY KEY (id),
    UNIQUE KEY idx_casbin_rule (ptype, v0, v1, v2, v3, v4, v5)
) ENGINE = InnoDB DEFAULT CHARSET = utf8mb4
