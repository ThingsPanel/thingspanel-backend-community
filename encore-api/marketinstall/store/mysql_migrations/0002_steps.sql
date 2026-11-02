CREATE TABLE market_install_steps (
 operation_id VARBINARY(1024) NOT NULL,
 ordinal INT NOT NULL,
 step_id VARBINARY(200) NOT NULL,
 idempotency_key VARBINARY(64) NOT NULL,
 payload_hash VARBINARY(64) NOT NULL,
 state VARBINARY(16) NOT NULL,
 receipt LONGTEXT NULL,
 updated_at DATETIME(6) NOT NULL DEFAULT CURRENT_TIMESTAMP(6),
 PRIMARY KEY (operation_id,ordinal),
 CONSTRAINT market_install_operation_step UNIQUE (operation_id,step_id),
 CONSTRAINT market_install_step_key UNIQUE (idempotency_key),
 CONSTRAINT market_install_step_operation FOREIGN KEY (operation_id) REFERENCES market_install_operations(id),
 CONSTRAINT market_install_ordinal CHECK (ordinal >= 0),
 CONSTRAINT market_install_step_state CHECK (state IN ('pending','dispatched','confirmed','unknown','rejected')),
 CONSTRAINT market_install_receipt_json CHECK (receipt IS NULL OR JSON_VALID(receipt)),
 CONSTRAINT market_install_step_hash CHECK (OCTET_LENGTH(payload_hash) = 64),
 CONSTRAINT market_install_idempotency_hash CHECK (OCTET_LENGTH(idempotency_key) = 64)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_bin;
