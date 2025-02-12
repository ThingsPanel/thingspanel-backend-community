-- ✅2025/2/11

CREATE TABLE public.open_api_keys (
	id varchar(36) NOT NULL,
	tenant_id varchar(36) NOT NULL,
	api_key varchar(200) NOT NULL,
	status int2 NULL,
	"name" varchar(200) NOT NULL,
	created_at timestamptz(6) NULL,
	updated_at timestamptz(6) NULL,
	CONSTRAINT open_api_keys_app_key_key UNIQUE (api_key),
	CONSTRAINT open_api_keys_pkey PRIMARY KEY (id)
);