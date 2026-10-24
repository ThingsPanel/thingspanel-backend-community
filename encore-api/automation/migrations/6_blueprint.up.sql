-- A18: where a rule came from.
--
-- A rule created from a template must remember which template version and which
-- parameter values produced it, otherwise "the template was updated" has no way
-- to find the rules it might affect, and an upgrade has nothing to compare
-- against.
--
-- The row stores the identity and the values, never a copy of the template body:
-- the compiled definition lives on auto_rule_revisions, so a template used by a
-- thousand rules is still one body plus a thousand small rows.

CREATE TABLE IF NOT EXISTS auto_blueprint_instances (
    rule_id            text PRIMARY KEY REFERENCES auto_rules(id) ON DELETE CASCADE,
    tenant_id          text NOT NULL,
    blueprint_key      text NOT NULL,
    blueprint_version  integer NOT NULL,
    blueprint_revision text NOT NULL,
    params             jsonb NOT NULL DEFAULT '{}'::jsonb,
    overrides          jsonb NOT NULL DEFAULT '{}'::jsonb,
    capabilities       jsonb NOT NULL DEFAULT '[]'::jsonb,
    created_at         timestamptz NOT NULL DEFAULT now(),
    upgraded_at        timestamptz
);

-- "Which rules use this template" is the question an upgrade or a deprecation
-- asks first, so it gets its own index instead of a scan of every rule.
CREATE INDEX IF NOT EXISTS auto_blueprint_instances_key
    ON auto_blueprint_instances (tenant_id, blueprint_key, blueprint_version);
