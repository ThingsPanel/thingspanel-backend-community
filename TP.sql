-- public.business definition

-- Drop table

-- DROP TABLE business;

CREATE TABLE business (
	id varchar(36) NOT NULL,
	"name" varchar(255) NULL,
	created_at int8 NULL,
	app_type varchar(255) NOT NULL DEFAULT ''::character varying, -- 应用类型
	app_id varchar(255) NOT NULL DEFAULT ''::character varying, -- app id
	app_secret varchar(255) NOT NULL DEFAULT ''::character varying, -- 密钥
	CONSTRAINT business_pkey PRIMARY KEY (id)
);

-- Column comments

COMMENT ON COLUMN public.business.app_type IS '应用类型';
COMMENT ON COLUMN public.business.app_id IS 'app id';
COMMENT ON COLUMN public.business.app_secret IS '密钥';


-- public.casbin_rule definition

-- Drop table

-- DROP TABLE casbin_rule;

CREATE TABLE casbin_rule (
	id bigserial NOT NULL,
	ptype varchar(100) NULL,
	v0 varchar(100) NULL,
	v1 varchar(100) NULL,
	v2 varchar(100) NULL,
	v3 varchar(100) NULL,
	v4 varchar(100) NULL,
	v5 varchar(100) NULL,
	v6 varchar(25) NULL,
	v7 varchar(25) NULL,
	CONSTRAINT casbin_rule_pkey PRIMARY KEY (id)
);
CREATE UNIQUE INDEX idx_casbin_rule ON public.casbin_rule USING btree (ptype, v0, v1, v2, v3, v4, v5, v6, v7);


-- public.conditions_log definition

-- Drop table

-- DROP TABLE conditions_log;

CREATE TABLE conditions_log (
	id varchar(36) NOT NULL,
	device_id varchar(36) NOT NULL,
	operation_type varchar(2) NULL, -- 操作类型1-定时触发 2-手动控制
	instruct varchar(255) NULL, -- 指令
	sender varchar(99) NULL, -- 发送者
	send_result varchar(2) NULL, -- 发送结果1-成功 2-失败
	respond varchar(255) NULL, -- 设备反馈
	cteate_time varchar(50) NULL,
	remark varchar(255) NULL,
	protocol_type varchar(50) NULL, -- mqtt,tcp
	CONSTRAINT conditions_log_pk PRIMARY KEY (id)
);
CREATE INDEX conditions_log_cteate_time_idx_desc ON public.conditions_log USING btree (cteate_time DESC);

-- Column comments

COMMENT ON COLUMN public.conditions_log.operation_type IS '操作类型1-定时触发 2-手动控制';
COMMENT ON COLUMN public.conditions_log.instruct IS '指令';
COMMENT ON COLUMN public.conditions_log.sender IS '发送者';
COMMENT ON COLUMN public.conditions_log.send_result IS '发送结果1-成功 2-失败';
COMMENT ON COLUMN public.conditions_log.respond IS '设备反馈';
COMMENT ON COLUMN public.conditions_log.protocol_type IS 'mqtt,tcp';


-- public.customers definition

-- Drop table

-- DROP TABLE customers;

CREATE TABLE customers (
	id varchar(36) NOT NULL,
	additional_info varchar NULL,
	address varchar NULL,
	address2 varchar NULL,
	city varchar(255) NULL DEFAULT ''::character varying,
	country varchar(255) NULL DEFAULT ''::character varying,
	email varchar(255) NULL DEFAULT ''::character varying,
	phone varchar(255) NULL DEFAULT ''::character varying,
	search_text varchar(255) NULL DEFAULT ''::character varying,
	state varchar(255) NULL DEFAULT ''::character varying,
	title varchar(255) NULL DEFAULT ''::character varying,
	zip varchar(255) NULL DEFAULT ''::character varying,
	CONSTRAINT customer_pkey PRIMARY KEY (id)
);


-- public.data_transpond definition

-- Drop table

-- DROP TABLE data_transpond;

CREATE TABLE data_transpond (
	id varchar(36) NOT NULL,
	process_id varchar(36) NULL, -- 流程id
	process_type varchar(36) NULL, -- 流程类型
	"label" varchar(255) NULL, -- 标签
	disabled varchar(10) NULL, -- 状态
	info varchar(255) NULL,
	env varchar(999) NULL,
	customer_id varchar(36) NULL,
	created_at int8 NULL,
	role_type varchar(2) NULL,
	CONSTRAINT data_transpond_pk PRIMARY KEY (id)
);



-- Column comments

COMMENT ON COLUMN public.data_transpond.process_id IS '流程id';
COMMENT ON COLUMN public.data_transpond.process_type IS '流程类型';
COMMENT ON COLUMN public.data_transpond."label" IS '标签';
COMMENT ON COLUMN public.data_transpond.disabled IS '状态';
COMMENT ON COLUMN public.data_transpond.role_type IS '1-接入引擎 2-数据转发';


-- public.logo definition

-- Drop table

-- DROP TABLE logo;

CREATE TABLE logo (
	id varchar(36) NOT NULL,
	system_name varchar(255) NULL, -- 系统名称
	theme varchar(99) NULL, -- 主题
	logo_one varchar(255) NULL, -- 首页logo
	logo_two varchar(255) NULL, -- 缓冲logo
	logo_three varchar(255) NULL,
	custom_id varchar(99) NULL,
	remark varchar(255) NULL,
	CONSTRAINT logo_pk PRIMARY KEY (id)
);

-- Column comments

COMMENT ON COLUMN public.logo.system_name IS '系统名称';
COMMENT ON COLUMN public.logo.theme IS '主题';
COMMENT ON COLUMN public.logo.logo_one IS '首页logo';
COMMENT ON COLUMN public.logo.logo_two IS '缓冲logo';


-- public.migrations definition

-- Drop table

-- DROP TABLE migrations;

CREATE TABLE migrations (
	id serial4 NOT NULL,
	migration varchar(255) NOT NULL,
	batch int4 NOT NULL,
	CONSTRAINT migrations_pkey PRIMARY KEY (id)
);


-- public.navigation definition

-- Drop table

-- DROP TABLE navigation;

CREATE TABLE navigation (
	id varchar(36) NOT NULL,
	"type" int4 NULL, -- 1:业务  2：自动化-控制策略 3：自动化-告警策略  4：可视化
	"name" varchar(255) NULL,
	"data" varchar(255) NULL,
	count int4 NULL DEFAULT 1, -- 数量
	CONSTRAINT navigation_pkey PRIMARY KEY (id)
);

-- Column comments

COMMENT ON COLUMN public.navigation."type" IS '1:业务  2：自动化-控制策略 3：自动化-告警策略  4：可视化';
COMMENT ON COLUMN public.navigation.count IS '数量';


-- public.operation_log definition

-- Drop table

-- DROP TABLE operation_log;

CREATE TABLE operation_log (
	id varchar(36) NOT NULL,
	"type" varchar(36) NULL,
	"describe" varchar(10000000) NULL,
	data_id varchar(36) NULL,
	created_at int8 NULL,
	detailed json NULL,
	CONSTRAINT operation_log_pkey PRIMARY KEY (id)
);
COMMENT ON TABLE public.operation_log IS '操作日志';


-- public.password_resets definition

-- Drop table

-- DROP TABLE password_resets;

CREATE TABLE password_resets (
	email varchar(255) NOT NULL,
	"token" varchar(255) NOT NULL,
	created_at timestamp(0) NULL
);


-- public.production definition

-- Drop table

-- DROP TABLE production;

CREATE TABLE production (
	id varchar(36) NOT NULL,
	"type" int4 NULL, -- 种植｜用药｜收获
	"name" varchar(255) NULL, -- 字段名
	value varchar(255) NULL, -- 值
	created_at int8 NULL,
	remark varchar(255) NULL, -- 备注
	insert_at int8 NULL,
	CONSTRAINT production_pkey PRIMARY KEY (id)
);

-- Column comments

COMMENT ON COLUMN public.production."type" IS '种植｜用药｜收获';
COMMENT ON COLUMN public.production."name" IS '字段名';
COMMENT ON COLUMN public.production.value IS '值';
COMMENT ON COLUMN public.production.remark IS '备注';


-- public.resources definition

-- Drop table

-- DROP TABLE resources;

CREATE TABLE resources (
	id varchar(36) NOT NULL,
	cpu varchar(36) NULL,
	mem varchar(36) NULL,
	created_at varchar(36) NULL,
	CONSTRAINT "Resources_pkey" PRIMARY KEY (id)
);
CREATE INDEX resources_created_at_idx ON public.resources USING btree (created_at DESC);


-- public.tp_function definition

-- Drop table

-- DROP TABLE tp_function;

CREATE TABLE tp_function (
	id varchar(36) NOT NULL,
	function_name varchar(99) NOT NULL,
	menu_id varchar(36) NULL,
	CONSTRAINT tp_function_un UNIQUE (function_name)
);


-- public.tp_menu definition

-- Drop table

-- DROP TABLE tp_menu;

CREATE TABLE tp_menu (
	id varchar(36) NOT NULL,
	menu_name varchar(99) NOT NULL,
	parent_id varchar(36) NOT NULL DEFAULT 0,
	remark varchar(255) NULL,
	CONSTRAINT tp_menu_pk PRIMARY KEY (id)
);


-- public.tp_role definition

-- Drop table

-- DROP TABLE tp_role;

CREATE TABLE tp_role (
	id varchar(36) NOT NULL,
	role_name varchar(99) NOT NULL,
	parent_id varchar(36) NULL DEFAULT 0,
	role_describe varchar(255) NULL,
	CONSTRAINT tp_role_pk PRIMARY KEY (id),
	CONSTRAINT tp_role_un UNIQUE (role_name)
);


-- public.tp_role_menu definition

-- Drop table

-- DROP TABLE tp_role_menu;

CREATE TABLE tp_role_menu (
	role_id varchar(36) NOT NULL,
	menu_id varchar(30) NOT NULL,
	CONSTRAINT tp_role_menu_pk PRIMARY KEY (role_id, menu_id)
);


-- public.ts_kv definition

-- Drop table

-- DROP TABLE ts_kv;

CREATE TABLE ts_kv (
	entity_type varchar(255) NOT NULL,
	entity_id varchar(36) NOT NULL,
	"key" varchar(255) NOT NULL,
	ts int8 NOT NULL,
	bool_v varchar(5) NULL,
	str_v text NULL,
	long_v int8 NULL,
	dbl_v float8 NULL,
	CONSTRAINT ts_kv_pkey PRIMARY KEY (entity_type, entity_id, key, ts)
);
CREATE INDEX ts_kv_ts_idx ON public.ts_kv USING btree (ts DESC);
COMMENT ON TABLE public.ts_kv IS '数据管理表';


-- public.ts_kv_latest definition

-- Drop table

-- DROP TABLE ts_kv_latest;

CREATE TABLE ts_kv_latest (
	entity_type varchar(255) NOT NULL,
	entity_id varchar(36) NOT NULL,
	"key" varchar(255) NOT NULL,
	ts int8 NOT NULL,
	bool_v varchar(5) NULL,
	str_v varchar(10000000) NULL,
	long_v int8 NULL,
	dbl_v float8 NULL,
	CONSTRAINT ts_kv_latest_pkey PRIMARY KEY (entity_type, entity_id, key)
);
CREATE UNIQUE INDEX "INDEX_KEY" ON public.ts_kv_latest USING btree (entity_type, entity_id, key);
COMMENT ON TABLE public.ts_kv_latest IS '最新数据';


-- public.ts_kv_test definition

-- Drop table

-- DROP TABLE ts_kv_test;

CREATE TABLE ts_kv_test (
	entity_type varchar(255) NOT NULL,
	entity_id varchar(36) NOT NULL,
	"key" varchar(255) NOT NULL,
	ts int8 NOT NULL,
	bool_v varchar(5) NULL,
	str_v text NULL,
	long_v int8 NULL,
	dbl_v float8 NULL,
	CONSTRAINT ts_kv_pkey2 PRIMARY KEY (entity_type, entity_id, key, ts)
);


-- public.users definition

-- Drop table

-- DROP TABLE users;

CREATE TABLE users (
	id varchar(36) NOT NULL,
	created_at int8 NOT NULL DEFAULT 0,
	updated_at int8 NOT NULL DEFAULT 0,
	enabled varchar(5) NULL,
	additional_info varchar NULL,
	authority varchar(255) NULL,
	customer_id varchar(36) NULL,
	email varchar(255) NULL,
	"password" varchar(255) NULL,
	"name" varchar(255) NULL,
	first_name varchar(255) NULL,
	last_name varchar(255) NULL,
	search_text varchar(255) NULL,
	email_verified_at int8 NOT NULL DEFAULT 0,
	remember_token varchar(100) NULL,
	mobile varchar(20) NULL,
	remark varchar(100) NULL,
	is_admin int8 NULL DEFAULT 0,
	business_id varchar(36) NULL DEFAULT 0, -- 业务id
	wx_openid varchar(50) NULL DEFAULT ''::character varying, -- 微信openid
	wx_unionid varchar(50) NULL DEFAULT ''::character varying, -- 微信unionid
	CONSTRAINT users_pkey PRIMARY KEY (id)
);
CREATE INDEX "INDEX_WX_OPENID" ON public.users USING btree (wx_openid);
COMMENT ON TABLE public.users IS '后台用户';

-- Column comments

COMMENT ON COLUMN public.users.business_id IS '业务id';
COMMENT ON COLUMN public.users.wx_openid IS '微信openid';
COMMENT ON COLUMN public.users.wx_unionid IS '微信unionid';


-- public.warning_config definition

-- Drop table

-- DROP TABLE warning_config;

CREATE TABLE warning_config (
	id varchar(36) NOT NULL,
	wid varchar(255) NOT NULL, -- 业务ID
	"name" varchar(255) NULL, -- 预警名称
	"describe" varchar(255) NULL, -- 预警描述
	config varchar(10000) NULL, -- 配置
	message varchar(1000) NULL, -- 消息模板
	bid varchar(255) NULL, -- 设备ID
	sensor varchar(100) NULL,
	customer_id varchar(36) NULL,
	other_message varchar(255) NULL, -- 其他信息
	CONSTRAINT warning_config_pkey PRIMARY KEY (id)
);
COMMENT ON TABLE public.warning_config IS '报警配置';



-- Column comments

COMMENT ON COLUMN public.warning_config.wid IS '业务ID';
COMMENT ON COLUMN public.warning_config."name" IS '预警名称';
COMMENT ON COLUMN public.warning_config."describe" IS '预警描述';
COMMENT ON COLUMN public.warning_config.config IS '配置';
COMMENT ON COLUMN public.warning_config.message IS '消息模板';
COMMENT ON COLUMN public.warning_config.bid IS '设备ID';
COMMENT ON COLUMN public.warning_config.other_message IS '其他信息';


-- public.warning_log definition

-- Drop table

-- DROP TABLE warning_log;

CREATE TABLE warning_log (
	id varchar(36) NOT NULL,
	"type" varchar(36) NULL,
	"describe" varchar(255) NULL,
	data_id varchar(36) NULL,
	created_at int8 NULL,
	CONSTRAINT warning_log_pkey PRIMARY KEY (id)
);
COMMENT ON TABLE public.warning_log IS '报警日志';
CREATE INDEX warning_log_created_at_idx ON public.warning_log (created_at DESC);

-- public.asset definition

-- Drop table

-- DROP TABLE asset;

CREATE TABLE asset (
	id varchar(36) NOT NULL,
	additional_info varchar NULL,
	customer_id varchar(36) NULL, -- 客户ID
	"name" varchar(255) NULL, -- 名称
	"label" varchar(255) NULL, -- 标签
	search_text varchar(255) NULL,
	"type" varchar(255) NULL, -- 类型
	parent_id varchar(36) NULL, -- 父级ID
	tier int4 NOT NULL DEFAULT 1, -- 层级
	business_id varchar(36) NULL, -- 业务ID
	CONSTRAINT asset_pkey PRIMARY KEY (id),
	CONSTRAINT asset_fk FOREIGN KEY (business_id) REFERENCES business(id) ON DELETE CASCADE ON UPDATE CASCADE
);
CREATE INDEX asset_parent_id_idx ON public.asset USING btree (parent_id);
COMMENT ON TABLE public.asset IS '资产';

-- Column comments

COMMENT ON COLUMN public.asset.customer_id IS '客户ID';
COMMENT ON COLUMN public.asset."name" IS '名称';
COMMENT ON COLUMN public.asset."label" IS '标签';
COMMENT ON COLUMN public.asset."type" IS '类型';
COMMENT ON COLUMN public.asset.parent_id IS '父级ID';
COMMENT ON COLUMN public.asset.tier IS '层级';
COMMENT ON COLUMN public.asset.business_id IS '业务ID';


-- public.conditions definition

-- Drop table

-- DROP TABLE conditions;

CREATE TABLE conditions (
	id varchar(36) NOT NULL,
	business_id varchar(36) NULL, -- 业务ID
	"name" varchar(255) NULL, -- 策略名称
	"describe" varchar(255) NULL, -- 策略描述
	status varchar(255) NULL, -- 策略状态
	config varchar(10000) NULL, -- 配置
	sort int8 NULL DEFAULT 100,
	"type" int8 NULL,
	issued varchar(20) NULL DEFAULT 0,
	customer_id varchar(36) NULL,
	CONSTRAINT conditions_pkey PRIMARY KEY (id),
	CONSTRAINT conditions_fk FOREIGN KEY (business_id) REFERENCES business(id) ON DELETE RESTRICT ON UPDATE CASCADE
);
COMMENT ON TABLE public.conditions IS '自动化规则';

-- Column comments

COMMENT ON COLUMN public.conditions.business_id IS '业务ID';
COMMENT ON COLUMN public.conditions."name" IS '策略名称';
COMMENT ON COLUMN public.conditions."describe" IS '策略描述';
COMMENT ON COLUMN public.conditions.status IS '策略状态';
COMMENT ON COLUMN public.conditions.config IS '配置';


-- public.dashboard definition

-- Drop table

-- DROP TABLE dashboard;

CREATE TABLE dashboard (
	id varchar(36) NOT NULL,
	"configuration" varchar(10000000) NULL,
	assigned_customers varchar(1000000) NULL,
	search_text varchar(255) NULL,
	title varchar(255) NULL,
	business_id varchar(36) NULL, -- 业务id
	CONSTRAINT dashboard_pkey PRIMARY KEY (id),
	CONSTRAINT dashboard_fk FOREIGN KEY (business_id) REFERENCES business(id) ON DELETE RESTRICT ON UPDATE CASCADE
);
COMMENT ON TABLE public.dashboard IS '仪表盘';

-- Column comments

COMMENT ON COLUMN public.dashboard.business_id IS '业务id';


-- public.device definition

-- Drop table

-- DROP TABLE device;

CREATE TABLE device (
	id varchar(36) NOT NULL,
	asset_id varchar(36) NULL, -- 资产id
	"token" varchar(255) NULL, -- 安全key
	additional_info varchar NULL, -- 存储基本配置
	customer_id varchar(36) NULL, -- 所属客户
	"type" varchar(255) NULL, -- 插件类型
	"name" varchar(255) NULL, -- 插件名
	"label" varchar(255) NULL,
	search_text varchar(255) NULL,
	"extension" varchar(50) NULL, -- 插件( 目录名)
	protocol varchar(50) NULL,
	port varchar(50) NULL,
	publish varchar(255) NULL,
	subscribe varchar(255) NULL,
	username varchar(255) NULL,
	"password" varchar(255) NULL,
	"location" varchar(255) NULL, -- 设备位置
	d_id varchar(255) NULL, -- 设备唯一标志
	CONSTRAINT device_pkey PRIMARY KEY (id),
	CONSTRAINT device_fk FOREIGN KEY (asset_id) REFERENCES asset(id) ON DELETE CASCADE ON UPDATE CASCADE
);
CREATE INDEX device_token_idx ON public.device USING btree (token);
COMMENT ON TABLE public.device IS '设备';

-- Column comments

COMMENT ON COLUMN public.device.asset_id IS '资产id';
COMMENT ON COLUMN public.device."token" IS '安全key';
COMMENT ON COLUMN public.device.additional_info IS '存储基本配置';
COMMENT ON COLUMN public.device.customer_id IS '所属客户';
COMMENT ON COLUMN public.device."type" IS '插件类型';
COMMENT ON COLUMN public.device."name" IS '插件名';
COMMENT ON COLUMN public.device."extension" IS '插件( 目录名)';
COMMENT ON COLUMN public.device."location" IS '设备位置';
COMMENT ON COLUMN public.device.d_id IS '设备唯一标志';


-- public.field_mapping definition

-- Drop table

-- DROP TABLE field_mapping;

CREATE TABLE field_mapping (
	id varchar(36) NOT NULL,
	device_id varchar(36) NULL,
	field_from varchar(255) NULL,
	field_to varchar(255) NULL,
	symbol varchar(255) NULL,
	CONSTRAINT field_mapping_pkey PRIMARY KEY (id),
	CONSTRAINT field_mapping_fk FOREIGN KEY (device_id) REFERENCES device(id) ON DELETE CASCADE ON UPDATE CASCADE
);


-- public.widget definition

-- Drop table

-- DROP TABLE widget;

CREATE TABLE widget (
	id varchar(36) NOT NULL,
	dashboard_id varchar(36) NULL,
	config varchar NULL,
	"type" varchar(255) NULL,
	"action" varchar(1000) NULL,
	updated_at timestamp(6) NULL,
	device_id varchar(36) NULL, -- 设备id
	widget_identifier varchar(255) NULL, -- 图表标识符如: environmentpanel:normal
	asset_id varchar(36) NULL,
	extend varchar(999) NULL, -- 扩展功能
	CONSTRAINT widget_pkey PRIMARY KEY (id),
	CONSTRAINT widget_fk FOREIGN KEY (dashboard_id) REFERENCES dashboard(id) ON DELETE CASCADE ON UPDATE CASCADE
);
COMMENT ON TABLE public.widget IS '图表';

-- Column comments

COMMENT ON COLUMN public.widget.device_id IS '设备id';
COMMENT ON COLUMN public.widget.widget_identifier IS '图表标识符如: environmentpanel:normal';
COMMENT ON COLUMN public.widget.extend IS '扩展功能';

CREATE TABLE chart (
	id varchar(36) NOT NULL,
	chart_type int NULL,
	chart_data json NULL,
	chart_name varchar(99) NULL,
	sort int NULL,
	issued varchar NULL DEFAULT 0,
	created_at int8 NULL,
	remark varchar(255) NULL,
	flag int NULL
);

-- Column comments

COMMENT ON COLUMN public.chart.chart_type IS '图表类型1-折线 2-仪表';
COMMENT ON COLUMN public.chart.chart_data IS '数据';
COMMENT ON COLUMN public.chart.chart_name IS '名称';
COMMENT ON COLUMN public.chart.sort IS '排序';
COMMENT ON COLUMN public.chart.issued IS '是否发布0-未发布1-已发布';
ALTER TABLE public.chart ALTER COLUMN issued TYPE int USING issued::int;
ALTER TABLE public.chart ADD CONSTRAINT chart_pk PRIMARY KEY (id);
ALTER TABLE public.chart ALTER COLUMN chart_type TYPE varchar(36) USING chart_type::varchar;



CREATE TABLE device_model (
	id varchar(36) NOT NULL,
	model_name varchar(255) NULL,
	flag int NULL,
	chart_data json NULL,
	model_type int NULL,
	"describe" varchar(255) NULL,
	"version" varchar(36) NULL,
	author varchar(36) NULL,
	sort int NULL,
	issued int NULL,
	remark varchar(255) NULL
);

-- Column comments

COMMENT ON COLUMN public.device_model.model_name IS '插件名称';
COMMENT ON COLUMN public.device_model.model_type IS '插件类型';
COMMENT ON COLUMN public.device_model."describe" IS '描述';
COMMENT ON COLUMN public.device_model."version" IS '版本';
ALTER TABLE public.device_model ADD created_at int8 NULL;
ALTER TABLE public.device_model ADD CONSTRAINT device_model_pk PRIMARY KEY (id);
ALTER TABLE public.device_model ALTER COLUMN model_type TYPE varchar(36) USING model_type::varchar;


CREATE TABLE tp_dict (
	id varchar(36) NOT NULL,
	dict_code varchar(36) NULL,
	dict_value varchar(99) NULL,
	"describe" varchar(99) NULL,
	created_at int8 NULL,
	CONSTRAINT tp_dict_pk PRIMARY KEY (id)
);

-- Column comments

COMMENT ON COLUMN public.tp_dict.dict_code IS '字典编码';
COMMENT ON COLUMN public.tp_dict.dict_value IS '值';
COMMENT ON COLUMN public.tp_dict."describe" IS '描述';

ALTER TABLE public.device DROP COLUMN "extension";
ALTER TABLE public.device ADD chart_option json NULL DEFAULT '{}';

COMMENT ON COLUMN public.device.chart_option IS '图表配置';


CREATE TABLE object_model (
	id varchar(36) NOT NULL,
	sort int4 NULL,
	object_describe varchar(255) NULL,
	object_name varchar(99) NOT NULL,
	object_type varchar(36) NOT NULL,
	object_data json NULL,
	created_at int8 NULL,
	remark varchar(255) NULL,
	CONSTRAINT object_model_pk PRIMARY KEY (id)
);

-- Column comments

COMMENT ON COLUMN public.object_model.object_name IS '物模型名称';
COMMENT ON COLUMN public.object_model.object_type IS '物模型类型';
COMMENT ON COLUMN public.object_model.object_data IS '物模型json';

ALTER TABLE public.device ADD device_type varchar(2) NOT NULL DEFAULT 1;
COMMENT ON COLUMN public.device.device_type IS '1-直连设备 2-网关设备 3-网关子设备';
ALTER TABLE public.device ADD parent_id varchar(36) NULL;
ALTER TABLE public.device ADD sub_protocol varchar(10) NULL;
COMMENT ON COLUMN public.device.sub_protocol IS 'modbus(TCP RTU)';
ALTER TABLE public.device ADD protocol_config json NULL DEFAULT '{}'::json;


CREATE TABLE public.tp_dashboard (
	id varchar(36) NOT NULL,
	relation_id varchar(36) NOT NULL,
	json_data json NULL DEFAULT '{}'::json,
	dashboard_name varchar(99) NULL,
	create_at int8 NULL,
	sort int NULL,
	remark varchar(255) NULL,
	CONSTRAINT tp_dashboard_pk PRIMARY KEY (id)
);

-- Column comments

COMMENT ON COLUMN public.tp_dashboard.sort IS '排序';




-- init sql
--24小时分区
SELECT create_hypertable('ts_kv', 'ts',chunk_time_interval => 86400000000);

INSERT INTO "users" ("id", "created_at", "updated_at", "enabled", "additional_info", "authority", "customer_id", "email", "password", "name", "first_name", "last_name", "search_text", "email_verified_at", "remember_token", "mobile", "remark", "is_admin", "business_id", "wx_openid", "wx_unionid") VALUES
('9212e9fb-a89c-4e35-9509-0a15df64f45a',	1606099326,	1623490224,	't',	NULL,	NULL,	NULL,	'super@super.cn',	'$2a$04$aGFaew.rkRmOUiOZ/3ZncO/HN1BuJc8Dcm1MNuU3HhbUVUgKIx7jG',	'Admin',	NULL,	NULL,	NULL,	0,	NULL,	'18618000000',	NULL,	0,	'',	'',	'');

INSERT INTO logo
(id, system_name, theme, logo_one, logo_two, logo_three, custom_id, remark)
VALUES('1d625cec-bf5b-2ad1-b135-a23b5fad05bf', 'ThingsPanel', 'blue', './files/logo/logo-one.svg', './files/logo/logo-two.gif', './files/logo/logo-three.png', '', '');
INSERT INTO tp_menu (id,menu_name,parent_id,remark) VALUES
	 ('1','homepage','0',NULL),
	 ('2','buisness','0',NULL),
	 ('3','dashboard','0',NULL),
	 ('4','automation','0',NULL),
	 ('5','alert_message','0',NULL),
	 ('6','system_log','0',NULL),
	 ('7','product_management','0',NULL),
	 ('9','data_management','0',NULL),
	 ('10','application_management','0',NULL),
	 ('11','user_management','0',NULL),
	 ('12','system_setup','0',NULL),
	 ('13','logs','6',NULL),
	 ('14','equipment_log','6',NULL),
	 ('15','firmware_upgrade','7',NULL),
	 ('8','data_switching','0',NULL);

ALTER TABLE public.tp_function ADD "path" varchar(255) NULL;
COMMENT ON COLUMN public.tp_function."path" IS '页面路径';
ALTER TABLE public.tp_function ADD name varchar(255) NULL;
COMMENT ON COLUMN public.tp_function.name IS '页面名称';
ALTER TABLE public.tp_function ADD component varchar(255) NULL;
COMMENT ON COLUMN public.tp_function.component IS '组件路径';
ALTER TABLE public.tp_function ADD title varchar(255) NULL;
COMMENT ON COLUMN public.tp_function.title IS '页面标题';
ALTER TABLE public.tp_function ADD icon varchar(255) NULL;
COMMENT ON COLUMN public.tp_function.icon IS '页面标题';
ALTER TABLE public.tp_function ADD "type" varchar(2) NULL;
COMMENT ON COLUMN public.tp_function."type" IS '类型0-目录 1-菜单 2-页面 3-按钮';
ALTER TABLE public.tp_function ADD function_code varchar(255) NULL;
COMMENT ON COLUMN public.tp_function.function_code IS '编码';
ALTER TABLE public.tp_function ADD parent_id varchar(36) NULL;

COMMENT ON COLUMN public.tp_function.icon IS '页面图表';
ALTER TABLE public.tp_function DROP CONSTRAINT tp_function_un;
ALTER TABLE public.tp_function ADD CONSTRAINT tp_function_pk PRIMARY KEY (id);
ALTER TABLE public.tp_function ADD sort int4 NULL;

INSERT INTO public.tp_function
(id, function_name, menu_id, "path", "name", component, title, icon, "type", function_code, parent_id, sort)
VALUES('c17a3b9e-bd1f-2f10-4c65-d2ae7030087b', '', NULL, '/alarm/list', 'Alarm', '/pages/alarm/AlarmIndex.vue', 'COMMON.WARNINFO', 'flaticon2-warning', '1', '', '0', 950);
INSERT INTO public.tp_function
(id, function_name, menu_id, "path", "name", component, title, icon, "type", function_code, parent_id, sort)
VALUES('a59eefbf-de02-a348-30af-d7f16053f884', '', NULL, '', 'system_log', '', 'COMMON.SYSTEMLOG', 'flaticon-open-box', '0', '', '0', 940);
INSERT INTO public.tp_function
(id, function_name, menu_id, "path", "name", component, title, icon, "type", function_code, parent_id, sort)
VALUES('39936c5b-14fd-588f-60be-77f422aa2d32', '', NULL, '', 'ProductManagment', '', 'COMMON.PRODUCTMANAGEMENT', 'menu-icon flaticon2-list', '0', '', '0', 930);
INSERT INTO public.tp_function
(id, function_name, menu_id, "path", "name", component, title, icon, "type", function_code, parent_id, sort)
VALUES('7c0c8fbb-6ba1-2323-511d-859c7923f954', '', NULL, '/log/list', 'LogList', '/pages/log/LogIndex.vue', 'COMMON.OPERATIONLOG', 'flaticon2-paper', '1', '', 'a59eefbf-de02-a348-30af-d7f16053f884', 999);
INSERT INTO public.tp_function
(id, function_name, menu_id, "path", "name", component, title, icon, "type", function_code, parent_id, sort)
VALUES('52a23456-775c-b731-7adf-a0fd3cddf649', '', NULL, '', 'BusinessAddButton', '', 'COMMON.NEWBUSINESS', '', '3', 'business:add', '83e18dcd-c6c8-eca2-2859-11dd6c6e7c6d', 999);
INSERT INTO public.tp_function
(id, function_name, menu_id, "path", "name", component, title, icon, "type", function_code, parent_id, sort)
VALUES('77d7133a-6434-bd51-232b-6b7fd862e50f', '', NULL, '', 'BusinessEdit', '', 'COMMON.EDITASSETSNAME', '', '3', 'business:edit', '83e18dcd-c6c8-eca2-2859-11dd6c6e7c6d', 998);
INSERT INTO public.tp_function
(id, function_name, menu_id, "path", "name", component, title, icon, "type", function_code, parent_id, sort)
VALUES('fd332720-1d06-9ba2-cf32-226cb2f54461', '', NULL, '', 'BusinessDel', '', 'COMMON.DELETE', '', '3', 'business:del', '83e18dcd-c6c8-eca2-2859-11dd6c6e7c6d', 997);
INSERT INTO public.tp_function
(id, function_name, menu_id, "path", "name", component, title, icon, "type", function_code, parent_id, sort)
VALUES('b143ccd9-eb65-655a-a41f-4311da5ed8c0', '', NULL, '/equipment/index', 'Equipment', '/pages/equipment/EquipmentIndex.vue', 'COMMON.EQUIPMENTLOG', 'flaticon-interface-3', '1', '', 'a59eefbf-de02-a348-30af-d7f16053f884', 998);
INSERT INTO public.tp_function
(id, function_name, menu_id, "path", "name", component, title, icon, "type", function_code, parent_id, sort)
VALUES('67b97839-919f-0976-2c79-c921adbec66e', '', NULL, '/strategy/alarmlist', 'AlarmStrategy', '/pages/automation/alarm/AlarmStrategy.vue', 'COMMON.ALARMSTRATEGY', '', '2', '', 'dce69d1d-8297-c5a4-1502-ace84dfe0209', 0);
INSERT INTO public.tp_function
(id, function_name, menu_id, "path", "name", component, title, icon, "type", function_code, parent_id, sort)
VALUES('8508677d-27ea-1158-c382-2bcf2b630346', '', NULL, '/strategy/strlist', 'ControlStrategy', '/pages/automation/control/ControlStrategy.vue', 'COMMON.CONTROLSTRATRGY', '', '2', '', 'dce69d1d-8297-c5a4-1502-ace84dfe0209', 0);
INSERT INTO public.tp_function
(id, function_name, menu_id, "path", "name", component, title, icon, "type", function_code, parent_id, sort)
VALUES('ec7a22ed-919d-7959-6737-145198f6172f', '', NULL, '/market', 'Market', '/pages/plugin/index.vue', 'COMMON.MARKET', 'flaticon2-supermarket', '1', '', '0', 910);
INSERT INTO public.tp_function
(id, function_name, menu_id, "path", "name", component, title, icon, "type", function_code, parent_id, sort)
VALUES('b4ad8251-ebdb-4c40-096a-eb74c59f7815', '', NULL, '', 'AddUser', '', 'COMMON.AddUSER', '', '3', 'sys:user:add', '2a1744d7-8440-c0a5-940a-9386ddfb1d0b', 0);
INSERT INTO public.tp_function
(id, function_name, menu_id, "path", "name", component, title, icon, "type", function_code, parent_id, sort)
VALUES('3f4348b0-f39d-ec42-14b4-623cbeadb12f', '', NULL, '/transpond/index', 'Transpond', '/pages/transpond/TranspondIndex.vue', 'COMMON.TRANSPOND', 'flaticon-upload-1', '1', '', '7cac14a0-0ff2-57d9-5465-597760bd2cb1', 998);
INSERT INTO public.tp_function
(id, function_name, menu_id, "path", "name", component, title, icon, "type", function_code, parent_id, sort)
VALUES('3786391a-6e8f-659d-1500-d2c3f82d6933', '', NULL, '/system/index', 'SystemSetup', '/pages/system/index.vue', 'COMMON.SYSTEMSETUP', 'flaticon-upload-1', '1', '', '4f2791e5-3c13-7249-c25f-77f6f787f574', 999);
INSERT INTO public.tp_function
(id, function_name, menu_id, "path", "name", component, title, icon, "type", function_code, parent_id, sort)
VALUES('2a1744d7-8440-c0a5-940a-9386ddfb1d0b', '', NULL, '/users/user', 'User', '/pages/users/UserIndex.vue', 'COMMON.USERS', 'flaticon2-user', '1', '', '4f2791e5-3c13-7249-c25f-77f6f787f574', 998);
INSERT INTO public.tp_function
(id, function_name, menu_id, "path", "name", component, title, icon, "type", function_code, parent_id, sort)
VALUES('7ce628ae-d494-d71c-9eb0-148e6bf47665', '', NULL, '/management/index', 'Management', '/pages/management/index.vue', 'COMMON.MANAGEMENT', 'flaticon-upload-1', '1', '', '4f2791e5-3c13-7249-c25f-77f6f787f574', 997);
INSERT INTO public.tp_function
(id, function_name, menu_id, "path", "name", component, title, icon, "type", function_code, parent_id, sort)
VALUES('a53dba0c-3388-0f49-35f3-6e56ff9acc68', '', NULL, '', 'DeviceManagment', '', 'COMMON.DEVICE', '', '3', 'business:device', '83e18dcd-c6c8-eca2-2859-11dd6c6e7c6d', 996);
INSERT INTO public.tp_function
(id, function_name, menu_id, "path", "name", component, title, icon, "type", function_code, parent_id, sort)
VALUES('6dab000b-7ced-a5ce-5fb0-5427f3bb8073', '', NULL, '/chart/list', 'ChartList', '/pages/chart/List.vue', 'COMMON.VISUALIZATION', 'flaticon2-laptop', '1', '', '0', 970);
INSERT INTO public.tp_function
(id, function_name, menu_id, "path", "name", component, title, icon, "type", function_code, parent_id, sort)
VALUES('539b8e97-b791-3260-8b23-1beca9497b19', '', NULL, '', 'AddPermission', '', 'COMMON.PERMISSIONADD', '', '3', 'sys:permission:add', '4231ea2c-a2fb-bd9c-8966-c7d654289deb', 0);
INSERT INTO public.tp_function
(id, function_name, menu_id, "path", "name", component, title, icon, "type", function_code, parent_id, sort)
VALUES('17f776f0-be0c-a216-a03a-00944865e8d7', '', NULL, '', 'EditPermission', '', 'COMMON.EDIT', '', '3', 'sys:permission:edit', '4231ea2c-a2fb-bd9c-8966-c7d654289deb', 0);
INSERT INTO public.tp_function
(id, function_name, menu_id, "path", "name", component, title, icon, "type", function_code, parent_id, sort)
VALUES('4231ea2c-a2fb-bd9c-8966-c7d654289deb', '', NULL, '/permission/index', 'PermissionManagement', '/pages/system/permissions/Index.vue', 'COMMON.PERMISSIONMANAGEMENT', 'flaticon-upload-1', '1', '', '4f2791e5-3c13-7249-c25f-77f6f787f574', 996);
INSERT INTO public.tp_function
(id, function_name, menu_id, "path", "name", component, title, icon, "type", function_code, parent_id, sort)
VALUES('363116a3-1c00-b875-1386-415ea0839849', '', NULL, '/list/device', 'device', '/pages/device/DeviceIndex.vue', 'COMMON.DEVICE', '', '2', '', 'a53dba0c-3388-0f49-35f3-6e56ff9acc68', 0);
INSERT INTO public.tp_function
(id, function_name, menu_id, "path", "name", component, title, icon, "type", function_code, parent_id, sort)
VALUES('b37757aa-3665-3d9d-994f-54e6ad37aff7', '', NULL, '', 'EditRole', '', 'COMMON.EDIT', '', '3', 'sys:role:edit', '7ce628ae-d494-d71c-9eb0-148e6bf47665', 0);
INSERT INTO public.tp_function
(id, function_name, menu_id, "path", "name", component, title, icon, "type", function_code, parent_id, sort)
VALUES('b9d3b307-917d-1914-6acc-0c2494a7c69c', '', NULL, '/product/list', 'ProductList', '/pages/product/managment/index.vue', 'COMMON.PRODUCTLIST', 'menu-icon flaticon2-list', '1', '', '39936c5b-14fd-588f-60be-77f422aa2d32', 999);
INSERT INTO public.tp_function
(id, function_name, menu_id, "path", "name", component, title, icon, "type", function_code, parent_id, sort)
VALUES('d8613453-278c-289c-6e18-ee58f6eb540b', '', NULL, '', 'DeletePermission', '', 'COMMON.DELETE', '', '3', 'sys:permission:del', '4231ea2c-a2fb-bd9c-8966-c7d654289deb', 0);
INSERT INTO public.tp_function
(id, function_name, menu_id, "path", "name", component, title, icon, "type", function_code, parent_id, sort)
VALUES('1988db79-dcb6-f8e5-4984-90e131efa526', '', NULL, '', 'SearchPermission', '', 'COMMON.SEARCH', '', '3', 'sys:permission:search', '4231ea2c-a2fb-bd9c-8966-c7d654289deb', 0);
INSERT INTO public.tp_function
(id, function_name, menu_id, "path", "name", component, title, icon, "type", function_code, parent_id, sort)
VALUES('a8ebb8af-adab-90fa-a553-49667370fc5f', '', NULL, '/access_engine/index', 'AccessEngine', '/pages/access-engine/AccessEngineIndex.vue', 'COMMON.NETWORKCOMPONENTS', 'flaticon-upload-1', '1', '', '7cac14a0-0ff2-57d9-5465-597760bd2cb1', 999);
INSERT INTO public.tp_function
(id, function_name, menu_id, "path", "name", component, title, icon, "type", function_code, parent_id, sort)
VALUES('9c4d044d-19c4-1b6c-c9d3-4d78e01ecb58', '', NULL, '/editpassword', 'EditPassword', '/pages/users/EditPassword.vue', 'COMMON.CHANGEPASSWORD', '', '3', 'sys:user:editpassword', '2a1744d7-8440-c0a5-940a-9386ddfb1d0b', 0);
INSERT INTO public.tp_function
(id, function_name, menu_id, "path", "name", component, title, icon, "type", function_code, parent_id, sort)
VALUES('59b4f53f-2e55-dc2b-a643-4a7fa62291a8', '', NULL, '', 'DelUser', '', 'COMMON.DELETE', '', '3', 'sys:user:del', '2a1744d7-8440-c0a5-940a-9386ddfb1d0b', 0);
INSERT INTO public.tp_function
(id, function_name, menu_id, "path", "name", component, title, icon, "type", function_code, parent_id, sort)
VALUES('5938f5ba-5970-759a-04c9-3595fd637c10', '', NULL, '', 'DelRole', '', 'COMMON.DELETE', '', '3', 'sys:role:del', '7ce628ae-d494-d71c-9eb0-148e6bf47665', 0);
INSERT INTO public.tp_function
(id, function_name, menu_id, "path", "name", component, title, icon, "type", function_code, parent_id, sort)
VALUES('065e4a85-aa03-4f59-0b00-8a7df1b03d87', '', NULL, '', 'AssignPermission', '', 'COMMON.PERMISSIONMANAGEMENT', '', '3', 'sys:role:assign', '7ce628ae-d494-d71c-9eb0-148e6bf47665', 0);
INSERT INTO public.tp_function
(id, function_name, menu_id, "path", "name", component, title, icon, "type", function_code, parent_id, sort)
VALUES('4f2791e5-3c13-7249-c25f-77f6f787f574', '', NULL, '', 'SystemManagement', '', 'COMMON.SYSTEMMANAGEMENT', 'flaticon2-gear', '0', '', '0', 900);
INSERT INTO public.tp_function
(id, function_name, menu_id, "path", "name", component, title, icon, "type", function_code, parent_id, sort)
VALUES('dce69d1d-8297-c5a4-1502-ace84dfe0209', '', NULL, '/strategy/list', 'StrategyList', '/pages/automation/AutomationIndex.vue', 'COMMON.AUTOMATION', 'flaticon2-hourglass', '1', '', '0', 960);
INSERT INTO public.tp_function
(id, function_name, menu_id, "path", "name", component, title, icon, "type", function_code, parent_id, sort)
VALUES('8ab87ef0-2e7b-c161-0e6b-0f59840e747f', '', NULL, '/device/watch', 'DeviceWatch', '/pages/device-watch/index.vue', '设备监控', 'flaticon2-rhombus', '1', '', '0', 990);
INSERT INTO public.tp_function
(id, function_name, menu_id, "path", "name", component, title, icon, "type", function_code, parent_id, sort)
VALUES('c7a4dbd4-3e40-7c48-819a-c4d447833dc3', '', NULL, '/visual/display', 'VisualDisplay', '/pages/visual/display/index.vue', 'COMMON.VISUALIZATIONSCREEN', '', '2', '', '6dab000b-7ced-a5ce-5fb0-5427f3bb8073', 0);
INSERT INTO public.tp_function
(id, function_name, menu_id, "path", "name", component, title, icon, "type", function_code, parent_id, sort)
VALUES('b7c0d632-776b-c374-f1cb-e857215ede00', '', NULL, '/product/batch/list', 'BatchList', '/pages/product/managment/batch/index.vue', 'COMMON.BATCHLIST', '', '2', '', '39936c5b-14fd-588f-60be-77f422aa2d32', 998);
INSERT INTO public.tp_function
(id, function_name, menu_id, "path", "name", component, title, icon, "type", function_code, parent_id, sort)
VALUES('7cac14a0-0ff2-57d9-5465-597760bd2cb1', '', NULL, '', 'RuleEngine', '', 'COMMON.RULEENGINE', 'flaticon2-gift-1', '0', '', '0', 920);
INSERT INTO public.tp_function
(id, function_name, menu_id, "path", "name", component, title, icon, "type", function_code, parent_id, sort)
VALUES('83e18dcd-c6c8-eca2-2859-11dd6c6e7c6d', '', NULL, '/list', 'BusinessList', '/pages/business/BusinessIndex.vue', '设备管理', 'flaticon2-rhombus', '1', '', '0', 999);
INSERT INTO public.tp_function
(id, function_name, menu_id, "path", "name", component, title, icon, "type", function_code, parent_id, sort)
VALUES('1bc93bad-41d3-ca37-638b-f79a29c1388b', '', NULL, '/data/index', 'Datas', '/pages/datas/DataIndex.vue', 'COMMON.DATAS', 'menu-icon flaticon2-list', '1', '', '0', 980);
-- INSERT INTO public.tp_function
-- (id, function_name, menu_id, "path", "name", component, title, icon, "type", function_code, parent_id, sort)
-- VALUES('2a7d5d94-62b5-c1c3-240b-cfeed8d92ec1', '', NULL, '/test123', 'Test123', '/pages/test123/index.vue', '设备地图', 'flaticon2-gear', '1', '', '0', 989);
INSERT INTO public.tp_function
(id, function_name, menu_id, "path", "name", component, title, icon, "type", function_code, parent_id, sort)
VALUES('6455296b-bce4-bd6a-8047-3788ff95f107', '', NULL, '', 'DelDevicePlugin', '', '删除设备插件', '', '3', 'plugin:device:del', 'ec7a22ed-919d-7959-6737-145198f6172f', 0);
INSERT INTO public.tp_function
(id, function_name, menu_id, "path", "name", component, title, icon, "type", function_code, parent_id, sort)
VALUES('39da5b03-2560-fc4f-d8ca-10374a6655eb', '', NULL, '', 'DelDevice', '', '删除设备', '', '3', 'device:del', 'a53dba0c-3388-0f49-35f3-6e56ff9acc68', 0);
INSERT INTO public.tp_function
(id, function_name, menu_id, "path", "name", component, title, icon, "type", function_code, parent_id, sort)
VALUES('9805e606-1c3e-565f-1380-d05eb1aeb0a9', '', NULL, '/device/watch/device_detail', 'DeviceDetail', '/pages/device-watch/device-detail/index.vue', 'COMMON.DEVICE_CHART', '', '2', '', '8ab87ef0-2e7b-c161-0e6b-0f59840e747f', 0);
INSERT INTO public.tp_function
(id, function_name, menu_id, "path", "name", component, title, icon, "type", function_code, parent_id, sort)
VALUES('7c7ef553-5342-f38e-6c07-222290f1c32d', '', NULL, '', 'DelProtocolPlugin', '', '删除协议插件', '', '3', 'plugin:protocol:del', 'ec7a22ed-919d-7959-6737-145198f6172f', 990);
INSERT INTO public.tp_function
(id, function_name, menu_id, "path", "name", component, title, icon, "type", function_code, parent_id, sort)
VALUES('8a2f3c67-0fe3-c18e-f17e-27b2b108e3c1', '', NULL, '', 'DelVisual', '', '删除可视化', '', '3', 'visual:del', '6dab000b-7ced-a5ce-5fb0-5427f3bb8073', 0);

INSERT INTO public.tp_role
(id, role_name, parent_id, role_describe)
VALUES('5b0969cb-ed0b-c664-1fab-d0ba90c39e04', '系统管理员', '', '');

INSERT INTO public.casbin_rule
(id, ptype, v0, v1, v2, v3, v4, v5, v6, v7)
VALUES(1391, 'g', 'super@super.cn', '5b0969cb-ed0b-c664-1fab-d0ba90c39e04', '', '', '', '', '', '');

INSERT INTO public.casbin_rule
(id, ptype, v0, v1, v2, v3, v4, v5, v6, v7)
VALUES(1651, 'p', '5b0969cb-ed0b-c664-1fab-d0ba90c39e04', '52a23456-775c-b731-7adf-a0fd3cddf649', 'allow', '', '', '', '', '');
INSERT INTO public.casbin_rule
(id, ptype, v0, v1, v2, v3, v4, v5, v6, v7)
VALUES(1652, 'p', '5b0969cb-ed0b-c664-1fab-d0ba90c39e04', '77d7133a-6434-bd51-232b-6b7fd862e50f', 'allow', '', '', '', '', '');
INSERT INTO public.casbin_rule
(id, ptype, v0, v1, v2, v3, v4, v5, v6, v7)
VALUES(1653, 'p', '5b0969cb-ed0b-c664-1fab-d0ba90c39e04', 'fd332720-1d06-9ba2-cf32-226cb2f54461', 'allow', '', '', '', '', '');
INSERT INTO public.casbin_rule
(id, ptype, v0, v1, v2, v3, v4, v5, v6, v7)
VALUES(1654, 'p', '5b0969cb-ed0b-c664-1fab-d0ba90c39e04', 'a53dba0c-3388-0f49-35f3-6e56ff9acc68', 'allow', '', '', '', '', '');
INSERT INTO public.casbin_rule
(id, ptype, v0, v1, v2, v3, v4, v5, v6, v7)
VALUES(1655, 'p', '5b0969cb-ed0b-c664-1fab-d0ba90c39e04', '363116a3-1c00-b875-1386-415ea0839849', 'allow', '', '', '', '', '');
INSERT INTO public.casbin_rule
(id, ptype, v0, v1, v2, v3, v4, v5, v6, v7)
VALUES(1656, 'p', '5b0969cb-ed0b-c664-1fab-d0ba90c39e04', '1bc93bad-41d3-ca37-638b-f79a29c1388b', 'allow', '', '', '', '', '');
INSERT INTO public.casbin_rule
(id, ptype, v0, v1, v2, v3, v4, v5, v6, v7)
VALUES(1657, 'p', '5b0969cb-ed0b-c664-1fab-d0ba90c39e04', '6dab000b-7ced-a5ce-5fb0-5427f3bb8073', 'allow', '', '', '', '', '');
INSERT INTO public.casbin_rule
(id, ptype, v0, v1, v2, v3, v4, v5, v6, v7)
VALUES(1658, 'p', '5b0969cb-ed0b-c664-1fab-d0ba90c39e04', 'c41bc15c-17d0-89d2-8f7d-5d32d7f2fc64', 'allow', '', '', '', '', '');
INSERT INTO public.casbin_rule
(id, ptype, v0, v1, v2, v3, v4, v5, v6, v7)
VALUES(1659, 'p', '5b0969cb-ed0b-c664-1fab-d0ba90c39e04', '9805e606-1c3e-565f-1380-d05eb1aeb0a9', 'allow', '', '', '', '', '');
INSERT INTO public.casbin_rule
(id, ptype, v0, v1, v2, v3, v4, v5, v6, v7)
VALUES(1660, 'p', '5b0969cb-ed0b-c664-1fab-d0ba90c39e04', 'dce69d1d-8297-c5a4-1502-ace84dfe0209', 'allow', '', '', '', '', '');
INSERT INTO public.casbin_rule
(id, ptype, v0, v1, v2, v3, v4, v5, v6, v7)
VALUES(1661, 'p', '5b0969cb-ed0b-c664-1fab-d0ba90c39e04', '67b97839-919f-0976-2c79-c921adbec66e', 'allow', '', '', '', '', '');
INSERT INTO public.casbin_rule
(id, ptype, v0, v1, v2, v3, v4, v5, v6, v7)
VALUES(1662, 'p', '5b0969cb-ed0b-c664-1fab-d0ba90c39e04', '8508677d-27ea-1158-c382-2bcf2b630346', 'allow', '', '', '', '', '');
INSERT INTO public.casbin_rule
(id, ptype, v0, v1, v2, v3, v4, v5, v6, v7)
VALUES(1663, 'p', '5b0969cb-ed0b-c664-1fab-d0ba90c39e04', 'c17a3b9e-bd1f-2f10-4c65-d2ae7030087b', 'allow', '', '', '', '', '');
INSERT INTO public.casbin_rule
(id, ptype, v0, v1, v2, v3, v4, v5, v6, v7)
VALUES(1664, 'p', '5b0969cb-ed0b-c664-1fab-d0ba90c39e04', 'a59eefbf-de02-a348-30af-d7f16053f884', 'allow', '', '', '', '', '');
INSERT INTO public.casbin_rule
(id, ptype, v0, v1, v2, v3, v4, v5, v6, v7)
VALUES(1665, 'p', '5b0969cb-ed0b-c664-1fab-d0ba90c39e04', '7c0c8fbb-6ba1-2323-511d-859c7923f954', 'allow', '', '', '', '', '');
INSERT INTO public.casbin_rule
(id, ptype, v0, v1, v2, v3, v4, v5, v6, v7)
VALUES(1666, 'p', '5b0969cb-ed0b-c664-1fab-d0ba90c39e04', 'b143ccd9-eb65-655a-a41f-4311da5ed8c0', 'allow', '', '', '', '', '');
INSERT INTO public.casbin_rule
(id, ptype, v0, v1, v2, v3, v4, v5, v6, v7)
VALUES(1667, 'p', '5b0969cb-ed0b-c664-1fab-d0ba90c39e04', '7cac14a0-0ff2-57d9-5465-597760bd2cb1', 'allow', '', '', '', '', '');
INSERT INTO public.casbin_rule
(id, ptype, v0, v1, v2, v3, v4, v5, v6, v7)
VALUES(1668, 'p', '5b0969cb-ed0b-c664-1fab-d0ba90c39e04', 'a8ebb8af-adab-90fa-a553-49667370fc5f', 'allow', '', '', '', '', '');
INSERT INTO public.casbin_rule
(id, ptype, v0, v1, v2, v3, v4, v5, v6, v7)
VALUES(1669, 'p', '5b0969cb-ed0b-c664-1fab-d0ba90c39e04', '3f4348b0-f39d-ec42-14b4-623cbeadb12f', 'allow', '', '', '', '', '');
INSERT INTO public.casbin_rule
(id, ptype, v0, v1, v2, v3, v4, v5, v6, v7)
VALUES(1670, 'p', '5b0969cb-ed0b-c664-1fab-d0ba90c39e04', 'ec7a22ed-919d-7959-6737-145198f6172f', 'allow', '', '', '', '', '');
INSERT INTO public.casbin_rule
(id, ptype, v0, v1, v2, v3, v4, v5, v6, v7)
VALUES(1671, 'p', '5b0969cb-ed0b-c664-1fab-d0ba90c39e04', '4f2791e5-3c13-7249-c25f-77f6f787f574', 'allow', '', '', '', '', '');
INSERT INTO public.casbin_rule
(id, ptype, v0, v1, v2, v3, v4, v5, v6, v7)
VALUES(1672, 'p', '5b0969cb-ed0b-c664-1fab-d0ba90c39e04', '3786391a-6e8f-659d-1500-d2c3f82d6933', 'allow', '', '', '', '', '');
INSERT INTO public.casbin_rule
(id, ptype, v0, v1, v2, v3, v4, v5, v6, v7)
VALUES(1673, 'p', '5b0969cb-ed0b-c664-1fab-d0ba90c39e04', '2a1744d7-8440-c0a5-940a-9386ddfb1d0b', 'allow', '', '', '', '', '');
INSERT INTO public.casbin_rule
(id, ptype, v0, v1, v2, v3, v4, v5, v6, v7)
VALUES(1674, 'p', '5b0969cb-ed0b-c664-1fab-d0ba90c39e04', '7ce628ae-d494-d71c-9eb0-148e6bf47665', 'allow', '', '', '', '', '');
INSERT INTO public.casbin_rule
(id, ptype, v0, v1, v2, v3, v4, v5, v6, v7)
VALUES(1675, 'p', '5b0969cb-ed0b-c664-1fab-d0ba90c39e04', 'b37757aa-3665-3d9d-994f-54e6ad37aff7', 'allow', '', '', '', '', '');
INSERT INTO public.casbin_rule
(id, ptype, v0, v1, v2, v3, v4, v5, v6, v7)
VALUES(1676, 'p', '5b0969cb-ed0b-c664-1fab-d0ba90c39e04', '5938f5ba-5970-759a-04c9-3595fd637c10', 'allow', '', '', '', '', '');
INSERT INTO public.casbin_rule
(id, ptype, v0, v1, v2, v3, v4, v5, v6, v7)
VALUES(1677, 'p', '5b0969cb-ed0b-c664-1fab-d0ba90c39e04', '065e4a85-aa03-4f59-0b00-8a7df1b03d87', 'allow', '', '', '', '', '');
INSERT INTO public.casbin_rule
(id, ptype, v0, v1, v2, v3, v4, v5, v6, v7)
VALUES(1678, 'p', '5b0969cb-ed0b-c664-1fab-d0ba90c39e04', '4231ea2c-a2fb-bd9c-8966-c7d654289deb', 'allow', '', '', '', '', '');
INSERT INTO public.casbin_rule
(id, ptype, v0, v1, v2, v3, v4, v5, v6, v7)
VALUES(1679, 'p', '5b0969cb-ed0b-c664-1fab-d0ba90c39e04', '1988db79-dcb6-f8e5-4984-90e131efa526', 'allow', '', '', '', '', '');


INSERT INTO public.tp_dict
(id, dict_code, dict_value, "describe", created_at)
VALUES('643f254a-0ac2-2616-c730-32c60dac7117', 'other_type', '1', '', 1663225360);
INSERT INTO public.tp_dict
(id, dict_code, dict_value, "describe", created_at)
VALUES('fd55cc73-427e-7dfc-121e-1e4f73b55e65', 'chart_type', '1', '传感器', 1663226829);
INSERT INTO public.tp_dict
(id, dict_code, dict_value, "describe", created_at)
VALUES('05fecef3-a1b1-4041-decf-59230f304fce', 'chart_type', '2', '控制器', 1663226845);
INSERT INTO public.tp_dict
(id, dict_code, dict_value, "describe", created_at)
VALUES('9d855e7b-c949-034f-4b96-f18ac03e0eb6', 'chart_type', '3', '照明', 1663226870);
INSERT INTO public.tp_dict
(id, dict_code, dict_value, "describe", created_at)
VALUES('c8bdaf38-d4da-5d29-4bf6-7e47ba497c88', 'chart_type', '4', '电力', 1663226875);
INSERT INTO public.tp_dict
(id, dict_code, dict_value, "describe", created_at)
VALUES('e197fbb7-b3b6-f33d-7c63-6d9fb1d60876', 'chart_type', '5', '摄像头', 1663226918);
INSERT INTO public.tp_dict
(id, dict_code, dict_value, "describe", created_at)
VALUES('4257cff7-ddf4-9977-a3ad-48630f5dc614', 'chart_type', '6', '其他', 1663226925);

INSERT INTO public.device_model
(id, model_name, flag, chart_data, model_type, "describe", "version", author, sort, issued, remark, created_at)
VALUES('49e25564-d0f5-6926-fae7-4d58726248c3', '开关', 0, '{"info":{"pluginName":"开关","pluginCategory":"2","author":"thingspanel","version":"v1.0.0","description":"设备字段：switch（int 1-开 0-关）；别名：开关"},"tsl":{"properties":[{"dataType":"integer","dataRange":"0-999","stepLength":0.1,"unit":"-","title":"开关","name":"switch"}],"option":{"classify":"custom","catValue":"relay"}},"chart":[{"componentName":"单控开关","type":"switch","series":[{"type":"switch","value":false,"id":1,"mapping":{"value":"switch","on":"1","off":"0","attr":{"dataType":"integer","dataRange":"0-999","stepLength":0.1,"unit":"-","title":"开关","name":"switch"}}}],"disabled":false,"name":"开关","controlType":"control","id":"QruyPTrD0AeN"}],"publish":{"isPub":false}}'::json, '2', '', 'v1.0.0', 'thingspanel', 0, 0, '', 1671700085);
INSERT INTO public.device_model
(id, model_name, flag, chart_data, model_type, "describe", "version", author, sort, issued, remark, created_at)
VALUES('5867753e-cb2d-32dc-a76d-7942d7ebcffc', '温湿度传感器', 0, '{"info":{"pluginName":"温湿度传感器","pluginCategory":"1","author":"thingspanel","version":"v1.0.0","description":"标准温湿度传感器"},"tsl":{"properties":[{"dataType":"float","dataRange":"0-999","stepLength":0.1,"unit":"%rh","name":"humidity","title":"湿度"},{"dataType":"float","dataRange":"0-999","stepLength":0.1,"unit":"℃","name":"temperature","title":"温度"}],"option":{"classify":"custom","catValue":"ambient_sensor"}},"chart":[{"series":[{"type":"gauge","progress":{"show":true,"width":18},"axisLine":{"lineStyle":{"width":2}},"axisTick":{"show":false},"splitLine":{"show":false,"length":5,"lineStyle":{"width":2,"color":"#999"}},"axisLabel":{"distance":10,"color":"#fff","fontSize":14},"anchor":{"show":true,"showAbove":true,"size":25,"itemStyle":{"borderWidth":10}},"title":{"show":false},"detail":{"fontSize":30,"offsetCenter":[0,"70%"],"color":"#fff"},"data":[{"value":0,"name":""}]}],"simulator":{"funcArr":["return +(Math.random() * 60).toFixed(2);"],"interval":5000},"name":"当前温度","mapping":["temperature"],"controlType":"dashboard","style":{"backgroundColor":"#2d3d86","opacity":1},"id":"bHEwRZGNTTYk"},{"series":[{"type":"gauge","progress":{"show":true,"width":18},"axisLine":{"lineStyle":{"width":2}},"axisTick":{"show":false},"splitLine":{"show":false,"length":5,"lineStyle":{"width":2,"color":"#999"}},"axisLabel":{"distance":10,"color":"#fff","fontSize":14},"anchor":{"show":true,"showAbove":true,"size":25,"itemStyle":{"borderWidth":10}},"title":{"show":false},"detail":{"fontSize":30,"offsetCenter":[0,"70%"],"color":"#fff"},"data":[{"value":0,"name":""}]}],"simulator":{"funcArr":["return +(Math.random() * 60).toFixed(2);"],"interval":5000},"name":"当前湿度","mapping":["humidity"],"controlType":"dashboard","style":{"backgroundColor":"#2d3d86","opacity":1},"id":"ap4aakzNhLEa"},{"xAxis":{"type":"category","axisLine":{"lineStyle":{"color":"#fff"}},"data":[""]},"yAxis":{"type":"value","axisLine":{"lineStyle":{"color":"#fff"}}},"series":[{"data":[0],"type":"line"}],"name":"温湿度历史数据","mapping":["humidity","temperature"],"controlType":"history","id":"qm9DsAYTktbN"}],"publish":{"isPub":false}}'::json, '1', '', 'v1.0.0', 'thingspanel', 0, 0, '', 1665748873);


ALTER TABLE public.device ADD sub_device_addr varchar(36) NULL;
COMMENT ON COLUMN public.device.sub_device_addr IS '子设备地址';

INSERT INTO public.tp_dict
(id, dict_code, dict_value, "describe", created_at)
VALUES('341efc2c-7704-7795-a53c-ecec34534832', 'GATEWAY_PROTOCOL', 'MQTT', 'MQTT协议', 1665998514);

CREATE TABLE public.tp_script (
	id varchar(36) NOT NULL,
	protocol_type varchar(99) NULL,
	script_name varchar(99) NULL,
	company varchar(99) NULL,
	product_name varchar(99) NULL,
	script_content text NULL,
	created_at int8 NULL,
	script_type varchar(99) NULL,
	remark varchar(255) NULL,
	CONSTRAINT tp_script_pk PRIMARY KEY (id)
);

-- Column comments

COMMENT ON COLUMN public.tp_script.protocol_type IS '协议类型';
COMMENT ON COLUMN public.tp_script.script_name IS '脚本名称';
COMMENT ON COLUMN public.tp_script.company IS '公司';
COMMENT ON COLUMN public.tp_script.product_name IS '产品名称';
COMMENT ON COLUMN public.tp_script.script_content IS '下行脚本';
COMMENT ON COLUMN public.tp_script.created_at IS '创建时间';
COMMENT ON COLUMN public.tp_script.script_type IS '脚本类型';
ALTER TABLE public.tp_script ALTER COLUMN script_type SET DEFAULT 'javascript';
ALTER TABLE public.tp_script ADD script_content_b text NULL;
COMMENT ON COLUMN public.tp_script.script_content_b IS '上行脚本';
ALTER TABLE public.tp_script RENAME COLUMN script_content TO script_content_a;

ALTER TABLE public.device ADD script_id varchar(36) NULL;
COMMENT ON COLUMN public.device.script_id IS '脚本id';

CREATE TABLE public.tp_product (
	id varchar(36) NOT NULL,
	name varchar(99) NOT NULL,
	serial_number varchar(99) NOT NULL,
	protocol_type varchar(36) NOT NULL,
	auth_type varchar(36) NOT NULL,
	plugin json NOT NULL DEFAULT '{}'::json,
	"describe" varchar(255) NULL,
	created_time int8 NULL,
	remark varchar(255) NULL,
	CONSTRAINT t_product_pk PRIMARY KEY (id),
	CONSTRAINT t_product_un UNIQUE (serial_number)
);

-- Column comments

COMMENT ON COLUMN public.tp_product.name IS '产品名称';
COMMENT ON COLUMN public.tp_product.serial_number IS '产品编号';
COMMENT ON COLUMN public.tp_product.protocol_type IS '协议类型';
COMMENT ON COLUMN public.tp_product.auth_type IS '认证方式';
COMMENT ON COLUMN public.tp_product.plugin IS '插件';

CREATE TABLE public.tp_batch (
	id varchar(36) NOT NULL,
	batch_number varchar(36) NOT NULL,
	product_id varchar(36) NOT NULL,
	device_number int NOT NULL,
	generate_flag varchar NOT NULL DEFAULT 0,
	"describe" varchar(255) NULL,
	created_time int8 NULL,
	remark varchar(255) NULL,
	CONSTRAINT t_batch_pk PRIMARY KEY (id),
	CONSTRAINT t_batch_un UNIQUE (batch_number,product_id),
	CONSTRAINT t_batch_fk FOREIGN KEY (product_id) REFERENCES public.tp_product(id) ON DELETE RESTRICT
);

-- Column comments

COMMENT ON COLUMN public.tp_batch.batch_number IS '批次编号';
COMMENT ON COLUMN public.tp_batch.product_id IS '产品id';
COMMENT ON COLUMN public.tp_batch.device_number IS '设备数量';
COMMENT ON COLUMN public.tp_batch.generate_flag IS '0-未生成 1-已生成';

CREATE TABLE public.tp_generate_device (
	id varchar(36) NOT NULL,
	batch_id varchar(36) NOT NULL,
	"token" varchar(36) NOT NULL,
	"password" varchar(36) NULL,
	activate_flag varchar(36) NOT NULL DEFAULT 0,
	activate_date varchar(36) NULL,
	device_id varchar(36) NULL,
	created_time int8 NULL,
	remark varchar(255) NULL,
	CONSTRAINT t_generate_device_pk PRIMARY KEY (id),
	CONSTRAINT t_generate_device_fk FOREIGN KEY (batch_id) REFERENCES public.tp_batch(id) ON DELETE CASCADE
);

-- Column comments

COMMENT ON COLUMN public.tp_generate_device.activate_flag IS '0-未激活 1-已激活';
COMMENT ON COLUMN public.tp_generate_device.activate_date IS '激活日期';

ALTER TABLE public.tp_batch ADD access_address varchar(36) NULL;
COMMENT ON COLUMN public.tp_batch.access_address IS '接入地址';

ALTER TABLE public.tp_product ADD device_model_id varchar(36) NULL;
COMMENT ON COLUMN public.tp_product.device_model_id IS '插件id';

CREATE TABLE public.tp_protocol_plugin (
	id varchar(36) NOT NULL,
	"name" varchar(99) NOT NULL,
	protocol_type varchar(36) NOT NULL,
	access_address varchar(255) NOT NULL,
	http_address varchar(255) NULL,
	sub_topic_prefix varchar(99) NULL,
	created_at int8 NULL,
	description varchar(255) NULL,
	CONSTRAINT tp_protocol_plugin_pk PRIMARY KEY (id)
);

-- Column comments
ALTER TABLE public.tp_protocol_plugin ADD device_type varchar(36) NULL;
COMMENT ON COLUMN public.tp_protocol_plugin.device_type IS '设备类型1-设备 2-网关';

COMMENT ON COLUMN public.tp_protocol_plugin.sub_topic_prefix IS '订阅主题前缀';
ALTER TABLE public.tp_protocol_plugin ADD CONSTRAINT tp_protocol_plugin_un UNIQUE (protocol_type,device_type);



INSERT INTO public.tp_protocol_plugin
(id, "name", protocol_type, access_address, http_address, sub_topic_prefix, created_at, description, device_type)
VALUES('c8a13166-e010-24e4-0565-e87feea162bb', 'MODBUS_TCP协议', 'MODBUS_TCP', '服务ip:502', '127.0.0.1:503', 'plugin/modbus/', 1668759820, '请参考文档对接设备,(应用管理->接入协议)docker部署将http服务器地址的ip改为172.19.0.8', '2');
INSERT INTO public.tp_protocol_plugin
(id, "name", protocol_type, access_address, http_address, sub_topic_prefix, created_at, description, device_type)
VALUES('2a95000c-9c29-7aae-58b0-5202daf1546a', 'MODBUS_RTU协议', 'MODBUS_RTU', '服务ip:502', '127.0.0.1:503', 'plugin/modbus/', 1668759841, '请参考文档对接设备,(应用管理->接入协议)docker部署将http服务器地址的ip改为172.19.0.8', '2');



-- 0.4.5
ALTER TABLE public.tp_dict ADD CONSTRAINT tp_dict_un UNIQUE (dict_code,dict_value);
ALTER TABLE public.ts_kv_latest ADD CONSTRAINT ts_kv_latest_fk FOREIGN KEY (entity_id) REFERENCES public.device(id) ON DELETE CASCADE;
ALTER TABLE public.conditions_log ADD CONSTRAINT conditions_log_fk FOREIGN KEY (device_id) REFERENCES public.device(id);
ALTER TABLE public.warning_config ADD CONSTRAINT warning_config_fk FOREIGN KEY (bid) REFERENCES public.device(id);
ALTER TABLE public.warning_log ADD CONSTRAINT warning_log_fk FOREIGN KEY (data_id) REFERENCES public.device(id);
ALTER TABLE public.tp_function ADD CONSTRAINT tp_function_fk FOREIGN KEY (menu_id) REFERENCES public.tp_menu(id);

INSERT INTO public.tp_dict
(id, dict_code, dict_value, "describe", created_at)
VALUES('19cd0e88-fe7b-a225-a0d6-77bd73757821', 'DRIECT_ATTACHED_PROTOCOL', 'mqtt', 'MQTT协议', 1669281205);
INSERT INTO public.tp_dict
(id, dict_code, dict_value, "describe", created_at)
VALUES('8881ffe7-7c2b-43c2-13f3-7227dafa46ba', 'DRIECT_ATTACHED_PROTOCOL', 'video_address', '视频地址接入', 1669281289);
ALTER TABLE public.device ADD created_at int8 NULL;


ALTER TABLE public.tp_script ADD device_type varchar(36) NOT NULL DEFAULT 1;
COMMENT ON COLUMN public.tp_script.device_type IS '设备类型';

ALTER TABLE public.conditions_log DROP CONSTRAINT conditions_log_fk;
ALTER TABLE public.conditions_log ADD CONSTRAINT conditions_log_fk FOREIGN KEY (device_id) REFERENCES public.device(id) ON DELETE CASCADE;

ALTER TABLE public.tp_dashboard ALTER COLUMN relation_id DROP NOT NULL;

INSERT INTO public.tp_dict
(id, dict_code, dict_value, "describe", created_at)
VALUES('8881ffe7-7c2b-43c2-13f3-7227dafa46bv', 'GATEWAY_PROTOCOL', 'MODBUS_TCP', 'MODBUS_TCP协议', 1669281289);
INSERT INTO public.tp_dict
(id, dict_code, dict_value, "describe", created_at)
VALUES('8881ffe7-7c2b-43c2-13f3-7227dafa46bs', 'GATEWAY_PROTOCOL', 'MODBUS_RTU', 'MODBUS_RTU协议', 1669281289);

INSERT INTO public.tp_protocol_plugin
(id, "name", protocol_type, access_address, http_address, sub_topic_prefix, created_at, description, device_type)
VALUES('de497b74-1bb6-2fc8-237b-75199304ba78', '自定义TCP协议', 'raw-tcp', '服务ip:7654', '127.0.0.1:8098', 'plugin/tcp/', 1670812659, 'docker部署不包含tcp协议插件服务,可根据文档自行部署', '2');
INSERT INTO public.tp_protocol_plugin
(id, "name", protocol_type, access_address, http_address, sub_topic_prefix, created_at, description, device_type)
VALUES('aea3b83a-284d-5738-6d0f-94fc73220c33', '官方TCP协议', 'tcp', '服务ip:7653', '127.0.0.1:8000', 'plugin/tcp/', 1670813735, 'docker部署不包含tcp协议插件服务,可根据文档自行部署', '1');
INSERT INTO public.tp_protocol_plugin
(id, "name", protocol_type, access_address, http_address, sub_topic_prefix, created_at, description, device_type)
VALUES('95b7c0b6-5c5b-4b45-c9ea-5bebda5a48ec', '官方TCP协议', 'tcp', '服务ip:7653', '127.0.0.1:8000', 'plugin/tcp/', 1670813749, 'docker部署不包含tcp协议插件服务,可根据文档自行部署', '2');
INSERT INTO public.tp_protocol_plugin
(id, "name", protocol_type, access_address, http_address, sub_topic_prefix, created_at, description, device_type)
VALUES('95c957bc-a53b-6445-e882-1973bb546b12', '自定义TCP协议', 'raw-tcp', '服务ip:7654', '127.0.0.1:8098', 'plugin/tcp/', 1670809899, 'docker部署不包含tcp协议插件服务,可根据文档自行部署', '1');

INSERT INTO public.tp_dict
(id, dict_code, dict_value, "describe", created_at)
VALUES('fad00d07-63c7-2685-1ee7-3e92d0142c88', 'DRIECT_ATTACHED_PROTOCOL', 'raw-tcp', '自定义TCP协议', 1670809899);
INSERT INTO public.tp_dict
(id, dict_code, dict_value, "describe", created_at)
VALUES('9663bb03-4881-1965-5cf5-17341a4db761', 'GATEWAY_PROTOCOL', 'raw-tcp', '自定义TCP协议', 1670812659);
INSERT INTO public.tp_dict
(id, dict_code, dict_value, "describe", created_at)
VALUES('b9249215-09a2-0298-02c2-0d9085fc40d2', 'DRIECT_ATTACHED_PROTOCOL', 'tcp', '官方TCP协议', 1670813735);
INSERT INTO public.tp_dict
(id, dict_code, dict_value, "describe", created_at)
VALUES('25074e80-b7ca-99a3-e1f7-2fec7ec31b24', 'GATEWAY_PROTOCOL', 'tcp', '官方TCP协议', 1670813749);

CREATE INDEX operation_log_created_at_idx ON public.operation_log (created_at DESC);
CREATE INDEX ts_kv_entity_id_idx ON public.ts_kv (entity_id,ts DESC);

--v0.4.6
ALTER TABLE public.logo DROP COLUMN custom_id;
ALTER TABLE public.logo ADD home_background varchar(255) NULL;
COMMENT ON COLUMN public.logo.home_background IS '首页背景';

ALTER TABLE public.tp_protocol_plugin ADD additional_info varchar(1000) NULL;
COMMENT ON COLUMN public.tp_protocol_plugin.additional_info IS '附加信息';

ALTER TABLE public.warning_log DROP CONSTRAINT warning_log_fk;
ALTER TABLE public.warning_log ADD CONSTRAINT warning_log_fk FOREIGN KEY (data_id) REFERENCES public.device(id) ON DELETE CASCADE ON UPDATE CASCADE;

INSERT INTO tp_dict
(id, dict_code, dict_value, "describe", created_at)
VALUES('9aa72824-e26b-2723-426a-ec8bcff091e9', 'GATEWAY_PROTOCOL', 'WVP_01', 'GB28181', 1673933847);

INSERT INTO tp_protocol_plugin
(id, "name", protocol_type, access_address, http_address, sub_topic_prefix, created_at, description, device_type, additional_info)
VALUES('1cd08053-f08a-8bda-2c22-c0b2582ce0b4', 'GB28181', 'WVP_01', '127.0.0.1:18080', 'http://127.0.0.1:18080||admin||admin', '-', 1673933847, '使用GB28181协议需要自行搭建wvp服务，然后按照http服务器地址样例修改（地址供平台后端调用）；协议类型必须以WVP_开头', '2', '[{"key":"域名称","value":""},{"key":"连接地址","value":""},{"key":"端口","value":""},{"key":"密码","value":""}]');

INSERT INTO tp_function
(id, function_name, menu_id, "path", "name", component, title, icon, "type", function_code, parent_id, sort)
VALUES('2a7d5d94-62b5-c1c3-240b-cfeed8d92ec1', '', NULL, '/test123', 'Test123', '/pages/test123/index.vue', 'MENU.DEVICE_MAP', 'flaticon2-gear', '1', '', '0', 989);

UPDATE tp_function
SET function_name='', menu_id=NULL, "path"='/device_map', "name"='DeviceMap', component='/pages/device-map/index.vue', title='MENU.DEVICE_MAP', icon='flaticon2-gear', "type"='1', function_code='', parent_id='0', sort=989
WHERE id='2a7d5d94-62b5-c1c3-240b-cfeed8d92ec1';

--v0.4.7
CREATE TABLE public.tp_automation (
	id varchar(36) NOT NULL,
	tenant_id varchar(36) NULL,
	automation_name varchar(99) NOT NULL,
	automation_described varchar(999) NULL,
	update_time int8 NULL,
	created_at int8 NOT NULL,
	created_by varchar(36) NULL,
	priority int NULL DEFAULT 50,
	enabled varchar(1) NOT NULL DEFAULT 0,
	remark varchar(255) NULL,
	CONSTRAINT tp_automation_pk PRIMARY KEY (id)
);

-- Column comments

COMMENT ON COLUMN public.tp_automation.automation_name IS '自动化名称';
COMMENT ON COLUMN public.tp_automation.automation_described IS '自动化描述';
COMMENT ON COLUMN public.tp_automation.priority IS '优先级';
COMMENT ON COLUMN public.tp_automation.enabled IS '启用状态0-未开启 1-已开启';

CREATE TABLE public.tp_automation_condition (
	id varchar(36) NOT NULL,
	automation_id varchar(36) NOT NULL,
	group_number int NULL,
	condition_type varchar(2) NOT NULL,
	device_id varchar(36) NULL,
	time_condition_type varchar(2) NULL,
	device_condition_type varchar(2) NULL,
	v1 varchar(99) NULL,
	v2 varchar(99) NULL,
	v3 varchar(99) NULL,
	v4 varchar(99) NULL,
	v5 varchar(99) NULL,
	remark varchar(255) NULL,
	CONSTRAINT tp_automation_condition_pk PRIMARY KEY (id),
	CONSTRAINT tp_automation_condition_fk FOREIGN KEY (automation_id) REFERENCES public.tp_automation(id) ON DELETE CASCADE ON UPDATE CASCADE,
	CONSTRAINT tp_automation_condition_fk_1 FOREIGN KEY (device_id) REFERENCES public.device(id) ON DELETE RESTRICT ON UPDATE CASCADE
);

-- Column comments

COMMENT ON COLUMN public.tp_automation_condition.group_number IS '小组编号';
COMMENT ON COLUMN public.tp_automation_condition.condition_type IS '条件类型1-设备条件 2-时间条件';
COMMENT ON COLUMN public.tp_automation_condition.time_condition_type IS '时间条件类型0-时间范围 1-单次 2-重复';
COMMENT ON COLUMN public.tp_automation_condition.device_condition_type IS '设备条件类型';

CREATE TABLE public.tp_automation_action (
	id varchar(36) NOT NULL,
	automation_id varchar(36) NOT NULL,
	action_type varchar(2) NOT NULL,
	device_id varchar(36) NULL,
	warning_strategy_id varchar(36) NULL,
	scenario_strategy_id varchar(36) NULL,
	additional_info varchar(999) NULL,
	remark varchar(255) NULL,
	CONSTRAINT tp_automation_action_pk PRIMARY KEY (id),
	CONSTRAINT tp_automation_action_fk FOREIGN KEY (automation_id) REFERENCES public.tp_automation(id) ON DELETE CASCADE ON UPDATE CASCADE
);

-- Column comments

COMMENT ON COLUMN public.tp_automation_action.action_type IS '动作类型1-设备输出 2-触发告警 3-激活场景';
COMMENT ON COLUMN public.tp_automation_action.additional_info IS '附加信息device_model1-设定属性 2-调动服务;instruct指令';

CREATE TABLE public.tp_warning_strategy (
	id varchar(36) NOT NULL,
	warning_strategy_name varchar(99) NOT NULL,
	warning_level varchar(2) NOT NULL,
	repeat_count int NULL,
	trigger_count int NULL,
	inform_way varchar(99) NULL,
	remark varchar(255) NULL,
	CONSTRAINT tp_warning_strategy_pk PRIMARY KEY (id)
);

-- Column comments

COMMENT ON COLUMN public.tp_warning_strategy.warning_level IS '告警级别';
COMMENT ON COLUMN public.tp_warning_strategy.repeat_count IS '重复次数';
COMMENT ON COLUMN public.tp_warning_strategy.trigger_count IS '已触发次数';
COMMENT ON COLUMN public.tp_warning_strategy.inform_way IS '通知方式';

CREATE TABLE public.tp_scenario_strategy (
	id varchar(36) NOT NULL,
	tenant_id varchar(36) NULL,
	scenario_name varchar(99) NOT NULL,
	scenario_description varchar(999) NULL,
	created_at int8 NOT NULL,
	created_by varchar(36) NULL,
	update_time int8 NULL,
	remark varchar(255) NULL,
	CONSTRAINT tp_scenario_strategy_pk PRIMARY KEY (id)
);

-- Column comments

COMMENT ON COLUMN public.tp_scenario_strategy.scenario_name IS '场景名称';
COMMENT ON COLUMN public.tp_scenario_strategy.scenario_description IS '场景描述';

CREATE TABLE public.tp_scenario_action (
	id varchar(36) NOT NULL,
	scenario_strategy_id varchar(36) NOT NULL,
	action_type varchar(2) NOT NULL DEFAULT 1,
	device_id varchar(36) NULL,
	device_model varchar(2) NULL,
	instruct varchar(999) NULL,
	remark varchar(255) NULL,
	CONSTRAINT tp_scenario_action_pk PRIMARY KEY (id),
	CONSTRAINT tp_scenario_action_fk FOREIGN KEY (scenario_strategy_id) REFERENCES public.tp_scenario_strategy(id) ON DELETE CASCADE ON UPDATE CASCADE,
	CONSTRAINT tp_scenario_action_fk_1 FOREIGN KEY (device_id) REFERENCES public.device(id) ON DELETE RESTRICT ON UPDATE CASCADE
);

-- Column comments

COMMENT ON COLUMN public.tp_scenario_action.action_type IS '动作类型 1-设备输出';
COMMENT ON COLUMN public.tp_scenario_action.device_model IS '模型类型1-设定属性 2-调动服务';
COMMENT ON COLUMN public.tp_scenario_action.instruct IS '指令';

CREATE TABLE public.tp_automation_log (
	id varchar(36) NOT NULL,
	automation_id varchar(36) NOT NULL,
	trigger_time int8 NULL,
	process_description varchar(999) NULL,
	process_result varchar(2) NULL, -- 执行状态 1-成功 2-失败
	remark varchar(255) NULL,
	CONSTRAINT tp_automation_log_pk PRIMARY KEY (id)
);

-- Column comments

COMMENT ON COLUMN public.tp_automation_log.process_result IS '执行状态 1-成功 2-失败';


-- public.tp_automation_log foreign keys

ALTER TABLE public.tp_automation_log ADD CONSTRAINT tp_automation_log_fk FOREIGN KEY (automation_id) REFERENCES public.tp_automation(id) ON DELETE CASCADE ON UPDATE CASCADE;

CREATE TABLE public.tp_automation_log_detail (
	id varchar(36) NOT NULL,
	automation_log_id varchar(36) NOT NULL,
	action_type varchar(2) NULL,
	process_description varchar(999) NULL,
	process_result varchar(2) NULL,
	remark varchar(255) NULL,
	target_id varchar(36) NULL,
	CONSTRAINT automation_log_detail_pk PRIMARY KEY (id),
	CONSTRAINT automation_log_detail_fk FOREIGN KEY (automation_log_id) REFERENCES public.tp_automation_log(id) ON DELETE CASCADE ON UPDATE CASCADE
);

-- Column comments

COMMENT ON COLUMN public.automation_log_detail.action_type IS '动作类型 1-设备输出 2-触发告警 3-激活场景';
COMMENT ON COLUMN public.automation_log_detail.process_result IS '执行状态 1-成功 2-失败';
COMMENT ON COLUMN public.automation_log_detail.target_id IS '设备id|告警id|场景id';

CREATE TABLE public.tp_warning_information (
	id varchar(36) NOT NULL,
	tenant_id varchar(36) NULL,
	warning_name varchar(99) NOT NULL,
	warning_level varchar(2) NULL,
	warning_description varchar(99) NULL,
	warning_content varchar(999) NULL,
	processing_result varchar(1) NULL,
	processing_instructions varchar(255) NULL,
	processing_time varchar(50) NULL,
	processing_people_id varchar(36) NULL,
	created_at int8 NULL,
	remark varchar(255) NULL,
	CONSTRAINT tp_warning_information_pk PRIMARY KEY (ip)
);

-- Column comments

COMMENT ON COLUMN public.tp_warning_information.warning_level IS '告警级别';
COMMENT ON COLUMN public.tp_warning_information.warning_description IS '告警描述';
COMMENT ON COLUMN public.tp_warning_information.warning_content IS '告警内容';
COMMENT ON COLUMN public.tp_warning_information.processing_result IS '处理结果 0-未处理 1-已处理 2-已忽略';
COMMENT ON COLUMN public.tp_warning_information.processing_instructions IS '处理说明';
COMMENT ON COLUMN public.tp_warning_information.processing_time IS '处理时间';
COMMENT ON COLUMN public.tp_warning_information.processing_people_id IS '处理人';
COMMENT ON COLUMN public.tp_warning_information.remark IS '备注';

CREATE TABLE public.tp_scenario_log (
	id varchar(36) NOT NULL,
	scenario_strategy_id varchar(36) NOT NULL,
	process_description varchar(99) NULL,
	trigger_time varchar(99) NULL,
	process_result varchar(2) NULL,
	remark varchar(255) NULL,
	CONSTRAINT tp_scenario_log_pk PRIMARY KEY (id),
	CONSTRAINT tp_scenario_log_fk FOREIGN KEY (scenario_strategy_id) REFERENCES public.tp_scenario_strategy(id) ON DELETE CASCADE ON UPDATE CASCADE
);

-- Column comments

COMMENT ON COLUMN public.tp_scenario_log.process_description IS '过程描述';
COMMENT ON COLUMN public.tp_scenario_log.process_result IS '执行状态 1-成功 2-失败';

ALTER TABLE public.tp_automation_log ALTER COLUMN trigger_time TYPE varchar(99) USING trigger_time::varchar;

CREATE TABLE public.tp_scenario_log_detail (
	id varchar(36) NOT NULL,
	scenario_log_id varchar(36) NOT NULL,
	action_type varchar(2) NULL,
	process_description varchar(99) NULL,
	process_result varchar(1) NULL,
	remark varchar(255) NULL,
	CONSTRAINT tp_scenario_log_detail_pk PRIMARY KEY (id),
	CONSTRAINT tp_scenario_log_detail_fk FOREIGN KEY (scenario_log_id) REFERENCES public.tp_scenario_log(id) ON DELETE CASCADE ON UPDATE CASCADE
);

-- Column comments

COMMENT ON COLUMN public.tp_scenario_log_detail.action_type IS '动作类型 1-设备输出';
COMMENT ON COLUMN public.tp_scenario_log_detail.process_result IS '执行状态 1-成功 2-失败';

ALTER TABLE public.tp_scenario_log_detail ADD target_id varchar(36) NULL;
COMMENT ON COLUMN public.tp_scenario_log_detail.target_id IS '设备id|告警id';
