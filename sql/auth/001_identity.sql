-- Authentication owns no second account database. Additive community migration.
ALTER TABLE users ADD COLUMN IF NOT EXISTS auth_version bigint NOT NULL DEFAULT 0 CHECK (auth_version >= 0);
ALTER TABLE app_push_devices ADD COLUMN IF NOT EXISTS auth_version bigint NOT NULL DEFAULT 0;
CREATE TABLE IF NOT EXISTS auth_external_bindings (
 id varchar(36) PRIMARY KEY,
 user_id varchar(36) NOT NULL REFERENCES users(id),
 issuer text NOT NULL,
 subject text NOT NULL,
 provider text NOT NULL,
 client_id text NOT NULL,
 enabled boolean NOT NULL DEFAULT true,
 created_at timestamptz NOT NULL DEFAULT now(),
 disabled_at timestamptz,
 rebind_until timestamptz,
 UNIQUE(issuer,subject)
);
CREATE INDEX IF NOT EXISTS auth_external_bindings_user ON auth_external_bindings(user_id);
CREATE TABLE IF NOT EXISTS auth_audit_events (
 id bigserial PRIMARY KEY,
 actor_id text NOT NULL,
 user_id text NOT NULL,
 event text NOT NULL,
 provider text NOT NULL,
 created_at timestamptz NOT NULL DEFAULT now()
);
