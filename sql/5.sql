-- ✅2025/2/11

CREATE TABLE public.open_api_keys (
	id varchar(36) NOT NULL,
	tenant_id varchar(36) NULL,
	app_key varchar(32) NULL,
	app_secret varchar(64) NULL,
	status int2 NULL,
	remark varchar(200) NULL,
	created_at timestamptz(6) NULL,
	updated_at timestamptz(6) NULL,
	CONSTRAINT open_api_keys_app_key_key UNIQUE (app_key),
	CONSTRAINT open_api_keys_pkey PRIMARY KEY (id)
);