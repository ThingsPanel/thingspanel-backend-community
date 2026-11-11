-- A08 owner only. MySQL 8.0.16+ / InnoDB; PostgreSQL history is unchanged.
-- Binary identities preserve opaque keys, case and trailing spaces. JSON text
-- retains number lexemes used by the original step digest contract.
CREATE TABLE market_install_operations (
 id VARBINARY(1024) NOT NULL,
 tenant_id VARBINARY(1024) NOT NULL,
 subject_id VARBINARY(1024) NOT NULL,
 authorization_ref TEXT NOT NULL,
 request_key VARBINARY(128) NOT NULL,
 request_digest VARBINARY(64) NOT NULL,
 artifact_digest VARBINARY(71) NOT NULL,
 plan LONGTEXT NOT NULL,
 state VARBINARY(16) NOT NULL,
 generation BIGINT NOT NULL DEFAULT 0,
 lease_until DATETIME(6) NULL,
 error_code VARCHAR(80) NOT NULL DEFAULT '',
 bundle_key TEXT NOT NULL,
 bundle_version TEXT NOT NULL,
 created_at DATETIME(6) NOT NULL DEFAULT CURRENT_TIMESTAMP(6),
 updated_at DATETIME(6) NOT NULL DEFAULT CURRENT_TIMESTAMP(6),
 PRIMARY KEY (id),
 CONSTRAINT market_install_tenant_key UNIQUE (tenant_id,request_key),
 CONSTRAINT market_install_operation_state CHECK (state IN ('queued','running','paused','completed','rejected')),
 CONSTRAINT market_install_generation CHECK (generation >= 0),
 CONSTRAINT market_install_plan_json CHECK (JSON_VALID(plan)),
 CONSTRAINT market_install_request_hash CHECK (OCTET_LENGTH(request_digest) = 64),
 CONSTRAINT market_install_artifact_hash CHECK (OCTET_LENGTH(artifact_digest) = 71),
 INDEX market_install_claim (state,lease_until,created_at)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_bin;
