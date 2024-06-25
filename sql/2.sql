CREATE TABLE service_plugins (
     id VARCHAR(36) PRIMARY KEY,
     name VARCHAR(255) NOT NULL,
     service_identifier VARCHAR(100) NOT NULL,
     service_type INT NOT NULL CHECK (service_type IN (1, 2)),
     last_active_time TIMESTAMP,
     version VARCHAR(100),
     create_at TIMESTAMP NOT NULL,
     update_at TIMESTAMP NOT NULL,
     description VARCHAR(255),
     service_config JSON,
     remark VARCHAR(255)
);

ALTER TABLE service_plugins
    ADD CONSTRAINT unique_service_identifier UNIQUE (service_identifier);

ALTER TABLE service_plugins
    ADD CONSTRAINT unique_name UNIQUE (name);

ALTER TABLE "public"."service_plugins"
ALTER COLUMN "create_at" TYPE timestamptz USING "create_at"::timestamptz,
  ALTER COLUMN "update_at" TYPE timestamptz USING "update_at"::timestamptz;


CREATE TABLE service_access (
    id VARCHAR(36) PRIMARY KEY,
    name VARCHAR(100) NOT NULL,
    service_plugin_id VARCHAR(36) NOT NULL,
    voucher VARCHAR(999) NOT NULL,
    description VARCHAR(255),
    service_access_config JSON,
    remark VARCHAR(255),
    CONSTRAINT fk_service_plugin
        FOREIGN KEY (service_plugin_id)
            REFERENCES service_plugins (id)
            ON DELETE RESTRICT
);

ALTER TABLE "public"."service_access"
    ADD COLUMN "create_at" timestamptz,
  ADD COLUMN "update_at" timestamptz,
  ADD COLUMN "tenant_id" varchar(36) NOT NULL;

ALTER TABLE "public"."service_access"
    ALTER COLUMN "create_at" SET NOT NULL,
ALTER COLUMN "update_at" SET NOT NULL;

ALTER TABLE "public"."devices"
    ADD COLUMN "service_access_id" varchar(36) NOT NULL,
    ADD CONSTRAINT fk_service_access_id FOREIGN KEY (service_access_id) REFERENCES public.service_access(id) ON DELETE RESTRICT;
ALTER TABLE public.devices ALTER COLUMN service_access_id DROP NOT NULL;
