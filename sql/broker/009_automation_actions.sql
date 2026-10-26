-- Local planning has no network side effects. A committed plan and its command
-- receipts are atomic; completing the effect does not mean a device executed it.
ALTER TABLE broker_ingress_effects ADD COLUMN IF NOT EXISTS attempts integer NOT NULL DEFAULT 0;
ALTER TABLE broker_ingress_effects ADD COLUMN IF NOT EXISTS retry_after timestamptz NOT NULL DEFAULT now();
CREATE TABLE IF NOT EXISTS broker_automation_policy_snapshots (
 tenant_id varchar(256) NOT NULL, scene_id varchar(256) NOT NULL,
 policy_sha256 varchar(64) NOT NULL, snapshot jsonb NOT NULL,
 created_at timestamptz NOT NULL DEFAULT now(),
 PRIMARY KEY(tenant_id,scene_id,policy_sha256)
);
CREATE TABLE IF NOT EXISTS broker_automation_actions (
 id uuid PRIMARY KEY, effect_id uuid NOT NULL REFERENCES broker_ingress_effects(id),
 tenant_id varchar(256) NOT NULL, scene_id varchar(256) NOT NULL,
 action_id varchar(256) NOT NULL, target_device_id varchar(256) NOT NULL,
 policy_sha256 varchar(64) NOT NULL, state varchar(24) NOT NULL
 CHECK(state IN ('accepted','skipped','unsupported','expired','fenced')),
 command_id uuid REFERENCES broker_command_requests(id), detail varchar(256) NOT NULL DEFAULT '',
 created_at timestamptz NOT NULL DEFAULT now(),
 UNIQUE(effect_id,scene_id,action_id,target_device_id)
);
CREATE INDEX IF NOT EXISTS broker_automation_command ON broker_automation_actions(command_id) WHERE command_id IS NOT NULL;
-- Preserve the existing scene+trigger-device token bucket: one token per 3s,
-- burst capacity 10 (initialize/limiter.go),
-- now atomically with the plan, including after a process restart.
CREATE TABLE IF NOT EXISTS broker_automation_execution_windows (
 tenant_id varchar(256) NOT NULL, scene_id varchar(256) NOT NULL,
 device_id varchar(256) NOT NULL, effect_id uuid NOT NULL REFERENCES broker_ingress_effects(id),
 last_evaluated_at timestamptz NOT NULL DEFAULT now(),
 tokens double precision NOT NULL DEFAULT 9 CHECK(tokens>=0 AND tokens<=10),
 PRIMARY KEY(tenant_id,scene_id,device_id)
);

ALTER TABLE broker_automation_execution_windows ADD COLUMN IF NOT EXISTS tokens double precision NOT NULL DEFAULT 9 CHECK(tokens>=0 AND tokens<=10);
