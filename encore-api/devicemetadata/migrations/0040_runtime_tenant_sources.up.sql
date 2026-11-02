-- No automatic backfill: historical UUID normalization cannot prove source
-- ownership. Only the local dual-session operator owner may provision a row.
CREATE TABLE runtime_tenant_sources (
 id UUID PRIMARY KEY,
 tenant_id UUID NOT NULL UNIQUE,
 source_tenant_id TEXT NOT NULL CHECK(length(source_tenant_id) BETWEEN 1 AND 128 AND source_tenant_id=btrim(source_tenant_id)),
 environment TEXT NOT NULL CHECK(length(environment) BETWEEN 1 AND 128),
 provider_id TEXT NOT NULL CHECK(length(provider_id) BETWEEN 1 AND 128),
 namespace TEXT NOT NULL CHECK(length(namespace) BETWEEN 1 AND 128),
 service_instance_id TEXT NOT NULL,
 community_origin TEXT NOT NULL,
 generation BIGINT NOT NULL DEFAULT 1 CHECK(generation=1),
 state TEXT NOT NULL DEFAULT 'active' CHECK(state IN ('active','revoked')),
 reviewed_manifest_sha256 TEXT NOT NULL CHECK(reviewed_manifest_sha256 ~ '^[0-9a-f]{64}$'),
 platform_actor_id TEXT NOT NULL,
 tenant_actor_id TEXT NOT NULL,
 created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
 revoked_at TIMESTAMPTZ,
 CHECK((state='revoked')=(revoked_at IS NOT NULL)),
 CHECK(platform_actor_id<>tenant_actor_id AND length(platform_actor_id)>0 AND length(tenant_actor_id)>0),
 UNIQUE(environment,provider_id,namespace,service_instance_id,community_origin,source_tenant_id),
 UNIQUE(tenant_id,id,generation)
);
CREATE TABLE runtime_tenant_source_audit (
 id UUID PRIMARY KEY,
 binding_id UUID NOT NULL REFERENCES runtime_tenant_sources(id),
 action TEXT NOT NULL CHECK(action IN ('provision','revoke')),
 platform_actor_id TEXT NOT NULL,
 tenant_actor_id TEXT NOT NULL,
 reviewed_manifest_sha256 TEXT NOT NULL CHECK(reviewed_manifest_sha256 ~ '^[0-9a-f]{64}$'),
 created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE FUNCTION guard_runtime_tenant_source() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
 IF TG_OP='DELETE' THEN RAISE EXCEPTION 'runtime tenant source cannot be deleted or reused'; END IF;
 IF (to_jsonb(NEW)-'state'-'revoked_at') IS DISTINCT FROM (to_jsonb(OLD)-'state'-'revoked_at')
   OR OLD.state<>'active' OR NEW.state<>'revoked' OR NEW.revoked_at IS NULL THEN
  RAISE EXCEPTION 'runtime tenant source cannot be rebound or revived';
 END IF;
 RETURN NEW;
END;$$;
CREATE TRIGGER runtime_tenant_source_immutable BEFORE UPDATE OR DELETE ON runtime_tenant_sources FOR EACH ROW EXECUTE FUNCTION guard_runtime_tenant_source();
CREATE FUNCTION guard_runtime_tenant_audit() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN RAISE EXCEPTION 'runtime tenant source audit is append-only'; END;$$;
CREATE TRIGGER runtime_tenant_source_audit_immutable BEFORE UPDATE OR DELETE ON runtime_tenant_source_audit FOR EACH ROW EXECUTE FUNCTION guard_runtime_tenant_audit();
ALTER TABLE device_runtime_outbox ADD COLUMN source_binding_id UUID;
ALTER TABLE device_runtime_outbox ADD COLUMN source_binding_generation BIGINT;
ALTER TABLE device_runtime_outbox ADD CONSTRAINT runtime_outbox_source_pair CHECK((source_binding_id IS NULL)=(source_binding_generation IS NULL));
ALTER TABLE device_runtime_outbox ADD CONSTRAINT runtime_outbox_source_fk FOREIGN KEY(tenant_id,source_binding_id,source_binding_generation) REFERENCES runtime_tenant_sources(tenant_id,id,generation);
CREATE FUNCTION guard_runtime_outbox_source() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
 IF NEW.source_binding_id IS DISTINCT FROM OLD.source_binding_id OR NEW.source_binding_generation IS DISTINCT FROM OLD.source_binding_generation THEN
  RAISE EXCEPTION 'runtime outbox source proof is immutable';
 END IF;
 RETURN NEW;
END;$$;
CREATE TRIGGER runtime_outbox_source_immutable BEFORE UPDATE ON device_runtime_outbox FOR EACH ROW EXECUTE FUNCTION guard_runtime_outbox_source();
