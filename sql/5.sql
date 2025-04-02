-- ✅2025/2/11 v1.1.5

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

INSERT INTO sys_ui_elements
(id, parent_id, element_code, element_type, orders, param1, param2, param3, authority, description, created_at, remark, multilingual, route_path)
VALUES('cf168132-3cde-a0e2-6772-e3a28fca4a59', 'e1ebd134-53df-3105-35f4-489fc674d173', 'management_api', 3, 1999, '/management/api', 'icon-park-outline:editor', '0', '["TENANT_ADMIN"]'::json, 'API key', '2025-02-14 18:38:42.007', '', 'route.management_api', '');
