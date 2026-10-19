-- Administrative route transitions share the existing binding and final-write
-- locks. These records are evidence/outbox, not another routing authority.
CREATE TABLE IF NOT EXISTS broker_route_members (
 broker_cell varchar(128) NOT NULL, role varchar(24) NOT NULL,
 instance_id varchar(128) NOT NULL, boot_id varchar(128) NOT NULL,
 generation uuid NOT NULL, heartbeat_at timestamptz NOT NULL DEFAULT now(),
 PRIMARY KEY(broker_cell,role,instance_id)
);
CREATE TABLE IF NOT EXISTS broker_route_changes (
 id uuid PRIMARY KEY, tenant_id varchar(36) NOT NULL, device_id varchar(36) NOT NULL,
 source_cell varchar(128) NOT NULL, target_cell varchar(128) NOT NULL,
 source_epoch bigint NOT NULL CHECK(source_epoch>0), actor varchar(128) NOT NULL,
 members_sha256 varchar(64) NOT NULL, plan_sha256 varchar(64) NOT NULL,
 phase varchar(24) NOT NULL CHECK(phase IN ('frozen','connecting','active','superseded')),
 created_at timestamptz NOT NULL DEFAULT now(), prepared_at timestamptz,
 activated_at timestamptz, target_ready_sha256 varchar(64),
 CHECK(source_cell<>target_cell)
);
CREATE UNIQUE INDEX IF NOT EXISTS broker_route_changes_open
 ON broker_route_changes(tenant_id,device_id) WHERE phase IN ('frozen','connecting');
CREATE TABLE IF NOT EXISTS broker_route_barrier_members (
 change_id uuid NOT NULL REFERENCES broker_route_changes(id),
 role varchar(24) NOT NULL, instance_id varchar(128) NOT NULL,
 boot_id varchar(128) NOT NULL, generation uuid NOT NULL,
 acknowledged_at timestamptz, evidence_sha256 varchar(64),
 PRIMARY KEY(change_id,role,instance_id)
);
CREATE TABLE IF NOT EXISTS broker_route_change_events (
 sequence bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
 change_id uuid NOT NULL REFERENCES broker_route_changes(id),
 stage varchar(32) NOT NULL, evidence jsonb NOT NULL,
 created_at timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX IF NOT EXISTS broker_route_change_events_change
 ON broker_route_change_events(change_id,sequence);
