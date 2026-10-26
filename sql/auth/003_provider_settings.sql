CREATE TABLE auth_provider_settings (
    id varchar(64) PRIMARY KEY,
    draft jsonb NOT NULL,
    draft_secret bytea NOT NULL,
    revision bigint NOT NULL DEFAULT 1,
    validated_revision bigint NOT NULL DEFAULT 0,
    tested_revision bigint NOT NULL DEFAULT 0,
    tested_at timestamptz,
    published jsonb,
    published_secret bytea,
    published_revision bigint NOT NULL DEFAULT 0,
    generation bigint NOT NULL DEFAULT 0,
    enabled boolean NOT NULL DEFAULT false,
    updated_at timestamptz NOT NULL DEFAULT now(),
    CHECK (revision > 0 AND published_revision >= 0 AND generation >= 0),
    CHECK (NOT enabled OR (published IS NOT NULL AND published_secret IS NOT NULL AND published_revision > 0))
);
