-- public.alarm_config definition

-- Drop table

-- DROP TABLE public.alarm_config;

CREATE TABLE public.alarm_config (
	id varchar(36) NOT NULL,
	"name" varchar(255) NOT NULL, -- 告警名称
	description varchar(255) NULL, -- 告警描述
	alarm_level varchar(10) NOT NULL, -- 告警级别H: 高M: 中L: 低
	notification_group_id varchar(36) NOT NULL, -- 通知组id
	created_at timestamptz(6) NOT NULL,
	updated_at timestamptz(6) NOT NULL,
	tenant_id varchar(36) NOT NULL,
	remark varchar(255) NULL,
	enabled varchar(10) NOT NULL, -- 是否启用Y-启用N-停止
	CONSTRAINT alarm_config_pk PRIMARY KEY (id)
);
COMMENT ON TABLE public.alarm_config IS '告警配置';

-- Column comments

COMMENT ON COLUMN public.alarm_config."name" IS '告警名称';
COMMENT ON COLUMN public.alarm_config.description IS '告警描述';
COMMENT ON COLUMN public.alarm_config.alarm_level IS '告警级别H: 高M: 中L: 低';
COMMENT ON COLUMN public.alarm_config.notification_group_id IS '通知组id';
COMMENT ON COLUMN public.alarm_config.enabled IS '是否启用Y-启用N-停止';


-- public.alarm_history definition

-- Drop table

-- DROP TABLE public.alarm_history;

CREATE TABLE public.alarm_history (
	id varchar(36) NOT NULL,
	alarm_config_id varchar(36) NOT NULL,
	group_id varchar(36) NOT NULL,
	scene_automation_id varchar(36) NOT NULL,
	"name" varchar(255) NOT NULL, -- 告警名称
	description varchar(255) NULL, -- 告警描述
	"content" text NULL, -- 内容（什么原因导致的告警）
	alarm_status varchar(3) NOT NULL, -- L 底 M中 H 高 N 正常
	tenant_id varchar(36) NOT NULL, -- 租户
	remark varchar(255) NULL,
	create_at timestamptz(6) NOT NULL, -- 创建时间
	alarm_device_list jsonb NOT NULL, -- 触发设备id
	CONSTRAINT alarm_history_pkey PRIMARY KEY (id)
);

-- Column comments

COMMENT ON COLUMN public.alarm_history."name" IS '告警名称';
COMMENT ON COLUMN public.alarm_history.description IS '告警描述';
COMMENT ON COLUMN public.alarm_history."content" IS '内容（什么原因导致的告警）';
COMMENT ON COLUMN public.alarm_history.alarm_status IS 'L 底 M中 H 高 N 正常';
COMMENT ON COLUMN public.alarm_history.tenant_id IS '租户';
COMMENT ON COLUMN public.alarm_history.create_at IS '创建时间';
COMMENT ON COLUMN public.alarm_history.alarm_device_list IS '触发设备id';


-- public.boards definition

-- Drop table

-- DROP TABLE public.boards;

CREATE TABLE public.boards (
	id varchar(36) NOT NULL, -- Id
	"name" varchar(255) NOT NULL, -- 看板名称
	config json NULL DEFAULT '{}'::json, -- 看板配置
	tenant_id varchar(36) NOT NULL, -- 租户id（唯一）
	created_at timestamptz(6) NOT NULL,
	updated_at timestamptz(6) NOT NULL,
	home_flag varchar(2) NOT NULL, -- 首页标志默认N，Y
	description varchar(500) NULL, -- 描述
	remark varchar(255) NULL, -- 备注
	menu_flag varchar(2) NULL, -- 菜单标志默认N，Y
	CONSTRAINT boards_pkey PRIMARY KEY (id)
);

-- Column comments

COMMENT ON COLUMN public.boards.id IS 'Id';
COMMENT ON COLUMN public.boards."name" IS '看板名称';
COMMENT ON COLUMN public.boards.config IS '看板配置';
COMMENT ON COLUMN public.boards.tenant_id IS '租户id（唯一）';
COMMENT ON COLUMN public.boards.home_flag IS '首页标志默认N，Y';
COMMENT ON COLUMN public.boards.description IS '描述';
COMMENT ON COLUMN public.boards.remark IS '备注';
COMMENT ON COLUMN public.boards.menu_flag IS '菜单标志默认N，Y';


-- public.casbin_rule definition

-- Drop table

-- DROP TABLE public.casbin_rule;

CREATE TABLE public.casbin_rule (
	id bigserial NOT NULL,
	ptype varchar(100) NULL,
	v0 varchar(100) NULL,
	v1 varchar(100) NULL,
	v2 varchar(100) NULL,
	v3 varchar(100) NULL,
	v4 varchar(100) NULL,
	v5 varchar(100) NULL,
	CONSTRAINT casbin_rule_pkey PRIMARY KEY (id)
);
CREATE UNIQUE INDEX idx_casbin_rule ON public.casbin_rule USING btree (ptype, v0, v1, v2, v3, v4, v5);


-- public.data_policy definition

-- Drop table

-- DROP TABLE public.data_policy;

CREATE TABLE public.data_policy (
	id varchar(36) NOT NULL, -- Id
	data_type varchar(1) NOT NULL, -- 清理类型:1-设备数据、2-操作日志
	retention_days int4 NOT NULL, -- 数据保留时间（天）
	last_cleanup_time timestamptz(6) NULL, -- 上次清理时间
	last_cleanup_data_time timestamptz(6) NULL, -- 上次清理的数据时间节点（实际清理的数据时间点）
	enabled varchar(1) NOT NULL, -- 是否启用：1启用 2停用
	remark varchar(255) NULL, -- 备注
	CONSTRAINT data_policy_pkey PRIMARY KEY (id)
);

-- Column comments

COMMENT ON COLUMN public.data_policy.id IS 'Id';
COMMENT ON COLUMN public.data_policy.data_type IS '清理类型:1-设备数据、2-操作日志';
COMMENT ON COLUMN public.data_policy.retention_days IS '数据保留时间（天）';
COMMENT ON COLUMN public.data_policy.last_cleanup_time IS '上次清理时间';
COMMENT ON COLUMN public.data_policy.last_cleanup_data_time IS '上次清理的数据时间节点（实际清理的数据时间点）';
COMMENT ON COLUMN public.data_policy.enabled IS '是否启用：1启用 2停用';
COMMENT ON COLUMN public.data_policy.remark IS '备注';


-- public.device_model_custom_commands definition

-- Drop table

-- DROP TABLE public.device_model_custom_commands;

CREATE TABLE public.device_model_custom_commands (
	id varchar(36) NOT NULL, -- id
	device_template_id varchar(36) NOT NULL, -- 设备模板id
	buttom_name varchar(255) NOT NULL, -- 按钮名称
	data_identifier varchar(255) NOT NULL, -- 数据标识符
	description varchar(500) NULL, -- 描述
	instruct text NULL, -- 指令内容
	enable_status varchar(10) NOT NULL, -- 启用状态enable-启用disable-禁用
	remark varchar(255) NULL, -- 备注
	tenant_id varchar NOT NULL,
	CONSTRAINT device_model_custom_commands_pk PRIMARY KEY (id)
);

-- Column comments

COMMENT ON COLUMN public.device_model_custom_commands.id IS 'id';
COMMENT ON COLUMN public.device_model_custom_commands.device_template_id IS '设备模板id';
COMMENT ON COLUMN public.device_model_custom_commands.buttom_name IS '按钮名称';
COMMENT ON COLUMN public.device_model_custom_commands.data_identifier IS '数据标识符';
COMMENT ON COLUMN public.device_model_custom_commands.description IS '描述';
COMMENT ON COLUMN public.device_model_custom_commands.instruct IS '指令内容';
COMMENT ON COLUMN public.device_model_custom_commands.enable_status IS '启用状态enable-启用disable-禁用';
COMMENT ON COLUMN public.device_model_custom_commands.remark IS '备注';


-- public.device_templates definition

-- Drop table

-- DROP TABLE public.device_templates;

CREATE TABLE public.device_templates (
	id varchar(36) NOT NULL, -- Id
	"name" varchar(255) NOT NULL, -- 模板名称
	author varchar(36) NULL DEFAULT ''::character varying, -- 作者
	"version" varchar(50) NULL DEFAULT ''::character varying, -- 版本号
	description varchar(500) NULL DEFAULT ''::character varying, -- 描述
	tenant_id varchar(36) NOT NULL DEFAULT ''::character varying,
	created_at timestamptz(6) NOT NULL,
	updated_at timestamptz(6) NOT NULL,
	flag int2 NULL DEFAULT 1, -- 标志 默认1
	"label" varchar(255) NULL, -- 标签
	web_chart_config json NULL, -- web图表配置
	app_chart_config json NULL, -- app图表配置
	remark varchar(255) NULL, -- 备注
	"path" varchar(999) NULL, -- 图片路径
	CONSTRAINT device_templates_pkey PRIMARY KEY (id)
);

-- Column comments

COMMENT ON COLUMN public.device_templates.id IS 'Id';
COMMENT ON COLUMN public.device_templates."name" IS '模板名称';
COMMENT ON COLUMN public.device_templates.author IS '作者';
COMMENT ON COLUMN public.device_templates."version" IS '版本号';
COMMENT ON COLUMN public.device_templates.description IS '描述';
COMMENT ON COLUMN public.device_templates.flag IS '标志 默认1';
COMMENT ON COLUMN public.device_templates."label" IS '标签';
COMMENT ON COLUMN public.device_templates.web_chart_config IS 'web图表配置';
COMMENT ON COLUMN public.device_templates.app_chart_config IS 'app图表配置';
COMMENT ON COLUMN public.device_templates.remark IS '备注';
COMMENT ON COLUMN public.device_templates."path" IS '图片路径';


-- public.device_user_logs definition

-- Drop table

-- DROP TABLE public.device_user_logs;

CREATE TABLE public.device_user_logs (
	id varchar(36) NOT NULL,
	device_nums int4 NOT NULL DEFAULT 0,
	device_on int4 NOT NULL DEFAULT 0,
	created_at timestamptz(6) NOT NULL DEFAULT CURRENT_TIMESTAMP,
	tenant_id varchar(36) NOT NULL, -- 租户 id
	CONSTRAINT device_user_logs_pkey PRIMARY KEY (id)
);

-- Column comments

COMMENT ON COLUMN public.device_user_logs.tenant_id IS '租户 id';


-- public."groups" definition

-- Drop table

-- DROP TABLE public."groups";

CREATE TABLE public."groups" (
	id varchar(36) NOT NULL,
	parent_id varchar(36) NULL DEFAULT 0, -- 默认0是父分组
	tier int4 NOT NULL DEFAULT 1, -- 层级 从1开始
	"name" varchar(255) NOT NULL, -- 分组名称
	description varchar(255) NULL, -- 描述
	created_at timestamptz(6) NOT NULL, -- 创建时间
	updated_at timestamptz(6) NOT NULL, -- 更新时间
	remark varchar(255) NULL,
	tenant_id varchar(36) NOT NULL, -- 租户id
	CONSTRAINT groups_pkey PRIMARY KEY (id)
);

-- Column comments

COMMENT ON COLUMN public."groups".parent_id IS '默认0是父分组';
COMMENT ON COLUMN public."groups".tier IS '层级 从1开始';
COMMENT ON COLUMN public."groups"."name" IS '分组名称';
COMMENT ON COLUMN public."groups".description IS '描述';
COMMENT ON COLUMN public."groups".created_at IS '创建时间';
COMMENT ON COLUMN public."groups".updated_at IS '更新时间';
COMMENT ON COLUMN public."groups".tenant_id IS '租户id';


-- public.logo definition

-- Drop table

-- DROP TABLE public.logo;

CREATE TABLE public.logo (
	id varchar(36) NOT NULL, -- Id
	system_name varchar(99) NOT NULL, -- 系统名称
	logo_cache varchar(255) NOT NULL, -- 站标Logo
	logo_background varchar(255) NOT NULL, -- 加载页面Logo
	logo_loading varchar(255) NOT NULL, -- 加载页面Logo
	home_background varchar(255) NOT NULL, -- 首页背景
	remark varchar(255) NULL,
	CONSTRAINT logo_pkey PRIMARY KEY (id)
);

-- Column comments

COMMENT ON COLUMN public.logo.id IS 'Id';
COMMENT ON COLUMN public.logo.system_name IS '系统名称';
COMMENT ON COLUMN public.logo.logo_cache IS '站标Logo';
COMMENT ON COLUMN public.logo.logo_background IS '加载页面Logo';
COMMENT ON COLUMN public.logo.logo_loading IS '加载页面Logo';
COMMENT ON COLUMN public.logo.home_background IS '首页背景';


-- public.notification_groups definition

-- Drop table

-- DROP TABLE public.notification_groups;

CREATE TABLE public.notification_groups (
	id varchar(36) NOT NULL,
	"name" varchar(99) NOT NULL, -- 名称
	notification_type varchar(25) NOT NULL, -- 通知类型MEMBER-成员通知 EMAIL-邮箱通知 SME-短信通知 VOICE-语音通知 WEBHOOK-webhook
	status varchar(10) NOT NULL, -- 通知状态ON-启用 OFF-停用
	notification_config jsonb NULL, -- 通知配置
	description varchar(255) NULL, -- 描述
	tenant_id varchar(36) NOT NULL, -- 租户id
	created_at timestamptz(6) NOT NULL, -- 创建时间
	updated_at timestamptz(6) NOT NULL, -- 更新时间
	remark varchar(255) NULL, -- 备注
	CONSTRAINT notification_groups_pkey PRIMARY KEY (id)
);

-- Column comments

COMMENT ON COLUMN public.notification_groups."name" IS '名称';
COMMENT ON COLUMN public.notification_groups.notification_type IS '通知类型MEMBER-成员通知 EMAIL-邮箱通知 SME-短信通知 VOICE-语音通知 WEBHOOK-webhook';
COMMENT ON COLUMN public.notification_groups.status IS '通知状态ON-启用 OFF-停用';
COMMENT ON COLUMN public.notification_groups.notification_config IS '通知配置';
COMMENT ON COLUMN public.notification_groups.description IS '描述';
COMMENT ON COLUMN public.notification_groups.tenant_id IS '租户id';
COMMENT ON COLUMN public.notification_groups.created_at IS '创建时间';
COMMENT ON COLUMN public.notification_groups.updated_at IS '更新时间';
COMMENT ON COLUMN public.notification_groups.remark IS '备注';


-- public.notification_histories definition

-- Drop table

-- DROP TABLE public.notification_histories;

CREATE TABLE public.notification_histories (
	id varchar(36) NOT NULL,
	send_time timestamptz(6) NOT NULL, -- 发送时间
	send_content text NULL, -- 发送内容
	send_target varchar(255) NOT NULL, -- 发送目标
	send_result varchar(25) NULL, -- 发送结果SUCCESS-成功FAILURE-失败
	notification_type varchar(25) NOT NULL, -- 通知类型MEMBER-成员通知 EMAIL-邮箱通知 SME-短信通知 VOICE-语音通知 WEBHOOK-webhook
	tenant_id varchar(36) NOT NULL, -- 租户id
	remark varchar(255) NULL, -- 备注
	CONSTRAINT notification_histories_pkey PRIMARY KEY (id)
);

-- Column comments

COMMENT ON COLUMN public.notification_histories.send_time IS '发送时间';
COMMENT ON COLUMN public.notification_histories.send_content IS '发送内容';
COMMENT ON COLUMN public.notification_histories.send_target IS '发送目标';
COMMENT ON COLUMN public.notification_histories.send_result IS '发送结果SUCCESS-成功FAILURE-失败';
COMMENT ON COLUMN public.notification_histories.notification_type IS '通知类型MEMBER-成员通知 EMAIL-邮箱通知 SME-短信通知 VOICE-语音通知 WEBHOOK-webhook';
COMMENT ON COLUMN public.notification_histories.tenant_id IS '租户id';
COMMENT ON COLUMN public.notification_histories.remark IS '备注';


-- public.notification_services_config definition

-- Drop table

-- DROP TABLE public.notification_services_config;

CREATE TABLE public.notification_services_config (
	id varchar(36) NOT NULL,
	config json NULL, -- 通知配置
	notice_type varchar(36) NOT NULL, -- 通知类型EMAIL-邮箱配置 SME-短信配置
	status varchar(36) NOT NULL, -- 状态 OPEN-开启 CLOSE-关闭
	remark varchar(255) NULL,
	CONSTRAINT notification_services_config_pkey PRIMARY KEY (id)
);

-- Column comments

COMMENT ON COLUMN public.notification_services_config.config IS '通知配置';
COMMENT ON COLUMN public.notification_services_config.notice_type IS '通知类型EMAIL-邮箱配置 SME-短信配置';
COMMENT ON COLUMN public.notification_services_config.status IS '状态 OPEN-开启 CLOSE-关闭';


-- public.operation_logs definition

-- Drop table

-- DROP TABLE public.operation_logs;

CREATE TABLE public.operation_logs (
	id varchar(36) NOT NULL, -- Id
	ip varchar(36) NOT NULL, -- 请求IP
	"path" varchar(2000) NULL, -- 请求url
	user_id varchar(36) NOT NULL, -- 操作用户
	"name" varchar(255) NULL, -- 接口名称
	created_at timestamptz(6) NOT NULL, -- 创建时间
	latency int8 NULL, -- 耗时(ms)
	request_message text NULL, -- 请求内容
	response_message text NULL, -- 响应内容
	tenant_id varchar(36) NOT NULL, -- 租户id
	remark varchar(255) NULL,
	CONSTRAINT operation_logs_pkey PRIMARY KEY (id)
);

-- Column comments

COMMENT ON COLUMN public.operation_logs.id IS 'Id';
COMMENT ON COLUMN public.operation_logs.ip IS '请求IP';
COMMENT ON COLUMN public.operation_logs."path" IS '请求url';
COMMENT ON COLUMN public.operation_logs.user_id IS '操作用户';
COMMENT ON COLUMN public.operation_logs."name" IS '接口名称';
COMMENT ON COLUMN public.operation_logs.created_at IS '创建时间';
COMMENT ON COLUMN public.operation_logs.latency IS '耗时(ms)';
COMMENT ON COLUMN public.operation_logs.request_message IS '请求内容';
COMMENT ON COLUMN public.operation_logs.response_message IS '响应内容';
COMMENT ON COLUMN public.operation_logs.tenant_id IS '租户id';


-- public.ota_upgrade_packages definition

-- Drop table

-- DROP TABLE public.ota_upgrade_packages;

CREATE TABLE public.ota_upgrade_packages (
	id varchar(36) NOT NULL, -- Id
	"name" varchar(200) NOT NULL, -- 升级包名称
	"version" varchar(36) NOT NULL, -- 升级包版本号
	target_version varchar(36) NULL, -- 待升级版本号
	device_config_id varchar(36) NOT NULL, -- 设备配置id
	"module" varchar(36) NULL, -- 模块名称
	package_type int2 NOT NULL, -- 升级包类型1-差分 2-整包
	signature_type varchar(36) NULL, -- 签名算法MD5 SHA256
	additional_info json NULL DEFAULT '{}'::json, -- 附加信息
	description varchar(500) NULL, -- 描述
	package_url varchar(500) NULL, -- 包下载路径
	created_at timestamptz(6) NOT NULL, -- 创建时间
	updated_at timestamptz(6) NULL, -- 修改时间
	remark varchar(255) NULL, -- 备注
	signature varchar(255) NULL, -- 升级包签名
	tenant_id varchar(36) NULL,
	CONSTRAINT ota_upgrade_packages_pkey PRIMARY KEY (id)
);

-- Column comments

COMMENT ON COLUMN public.ota_upgrade_packages.id IS 'Id';
COMMENT ON COLUMN public.ota_upgrade_packages."name" IS '升级包名称';
COMMENT ON COLUMN public.ota_upgrade_packages."version" IS '升级包版本号';
COMMENT ON COLUMN public.ota_upgrade_packages.target_version IS '待升级版本号';
COMMENT ON COLUMN public.ota_upgrade_packages.device_config_id IS '设备配置id';
COMMENT ON COLUMN public.ota_upgrade_packages."module" IS '模块名称';
COMMENT ON COLUMN public.ota_upgrade_packages.package_type IS '升级包类型1-差分 2-整包';
COMMENT ON COLUMN public.ota_upgrade_packages.signature_type IS '签名算法MD5 SHA256';
COMMENT ON COLUMN public.ota_upgrade_packages.additional_info IS '附加信息';
COMMENT ON COLUMN public.ota_upgrade_packages.description IS '描述';
COMMENT ON COLUMN public.ota_upgrade_packages.package_url IS '包下载路径';
COMMENT ON COLUMN public.ota_upgrade_packages.created_at IS '创建时间';
COMMENT ON COLUMN public.ota_upgrade_packages.updated_at IS '修改时间';
COMMENT ON COLUMN public.ota_upgrade_packages.remark IS '备注';
COMMENT ON COLUMN public.ota_upgrade_packages.signature IS '升级包签名';


-- public.ota_upgrade_tasks definition

-- Drop table

-- DROP TABLE public.ota_upgrade_tasks;

CREATE TABLE public.ota_upgrade_tasks (
	id varchar(36) NOT NULL, -- Id
	"name" varchar(200) NOT NULL, -- 任务名称
	ota_upgrade_package_id varchar(36) NOT NULL, -- 升级包id（外键，关联删除）
	description varchar(500) NULL, -- 描述
	created_at timestamptz(6) NOT NULL, -- 创建时间
	remark varchar(255) NULL, -- 备注
	CONSTRAINT ota_upgrade_tasks_pkey PRIMARY KEY (id)
);

-- Column comments

COMMENT ON COLUMN public.ota_upgrade_tasks.id IS 'Id';
COMMENT ON COLUMN public.ota_upgrade_tasks."name" IS '任务名称';
COMMENT ON COLUMN public.ota_upgrade_tasks.ota_upgrade_package_id IS '升级包id（外键，关联删除）';
COMMENT ON COLUMN public.ota_upgrade_tasks.description IS '描述';
COMMENT ON COLUMN public.ota_upgrade_tasks.created_at IS '创建时间';
COMMENT ON COLUMN public.ota_upgrade_tasks.remark IS '备注';


-- public.protocol_plugins definition

-- Drop table

-- DROP TABLE public.protocol_plugins;

CREATE TABLE public.protocol_plugins (
	id varchar(36) NOT NULL, -- Id
	"name" varchar(36) NOT NULL, -- 插件名称
	device_type int2 NOT NULL DEFAULT 1, -- 接入设备类型 (1-直连设备 2-网关设备 默认直连设备)
	protocol_type varchar(50) NOT NULL, -- 协议类型
	access_address varchar(500) NULL, -- 接入地址
	http_address varchar(500) NULL, -- HTTP服务地址
	sub_topic_prefix varchar(500) NULL, -- 插件订阅前缀
	description varchar(500) NULL, -- 描述
	additional_info varchar(1000) NULL, -- 附加信息
	created_at timestamptz(6) NOT NULL, -- 创建时间
	update_at timestamptz(6) NOT NULL, -- 更新时间
	remark varchar(255) NULL, -- 备注
	CONSTRAINT protocol_plugins_pkey PRIMARY KEY (id)
);

-- Column comments

COMMENT ON COLUMN public.protocol_plugins.id IS 'Id';
COMMENT ON COLUMN public.protocol_plugins."name" IS '插件名称';
COMMENT ON COLUMN public.protocol_plugins.device_type IS '接入设备类型 (1-直连设备 2-网关设备 默认直连设备)';
COMMENT ON COLUMN public.protocol_plugins.protocol_type IS '协议类型';
COMMENT ON COLUMN public.protocol_plugins.access_address IS '接入地址';
COMMENT ON COLUMN public.protocol_plugins.http_address IS 'HTTP服务地址';
COMMENT ON COLUMN public.protocol_plugins.sub_topic_prefix IS '插件订阅前缀';
COMMENT ON COLUMN public.protocol_plugins.description IS '描述';
COMMENT ON COLUMN public.protocol_plugins.additional_info IS '附加信息';
COMMENT ON COLUMN public.protocol_plugins.created_at IS '创建时间';
COMMENT ON COLUMN public.protocol_plugins.update_at IS '更新时间';
COMMENT ON COLUMN public.protocol_plugins.remark IS '备注';


-- public.roles definition

-- Drop table

-- DROP TABLE public.roles;

CREATE TABLE public.roles (
	id varchar(36) NOT NULL, -- Id
	"name" varchar(99) NOT NULL, -- 名称
	description varchar(255) NULL, -- 描述
	created_at timestamp NULL, -- 创建时间
	updated_at timestamp NULL, -- 更新时间
	tenant_id varchar(36) NULL, -- 租户id
	CONSTRAINT roles_pkey PRIMARY KEY (id)
);

-- Column comments

COMMENT ON COLUMN public.roles.id IS 'Id';
COMMENT ON COLUMN public.roles."name" IS '名称';
COMMENT ON COLUMN public.roles.description IS '描述';
COMMENT ON COLUMN public.roles.created_at IS '创建时间';
COMMENT ON COLUMN public.roles.updated_at IS '更新时间';
COMMENT ON COLUMN public.roles.tenant_id IS '租户id';


-- public.scene_automations definition

-- Drop table

-- DROP TABLE public.scene_automations;

CREATE TABLE public.scene_automations (
	id varchar(36) NOT NULL, -- 联动
	"name" varchar(255) NOT NULL, -- 名称
	description varchar(255) NULL, -- 描述
	enabled varchar(10) NOT NULL, -- 是否启用 Y：启用 N：停用
	tenant_id varchar(36) NOT NULL, -- 租户ID
	creator varchar(36) NOT NULL, -- 创建人id
	updator varchar(36) NOT NULL, -- 修改人id
	created_at timestamptz(6) NOT NULL, -- 创建时间
	updated_at timestamptz(6) NULL, -- 更新时间
	remark varchar(255) NULL,
	CONSTRAINT scene_automations_pkey PRIMARY KEY (id)
);

-- Column comments

COMMENT ON COLUMN public.scene_automations.id IS '联动';
COMMENT ON COLUMN public.scene_automations."name" IS '名称';
COMMENT ON COLUMN public.scene_automations.description IS '描述';
COMMENT ON COLUMN public.scene_automations.enabled IS '是否启用 Y：启用 N：停用';
COMMENT ON COLUMN public.scene_automations.tenant_id IS '租户ID';
COMMENT ON COLUMN public.scene_automations.creator IS '创建人id';
COMMENT ON COLUMN public.scene_automations.updator IS '修改人id';
COMMENT ON COLUMN public.scene_automations.created_at IS '创建时间';
COMMENT ON COLUMN public.scene_automations.updated_at IS '更新时间';


-- public.scene_info definition

-- Drop table

-- DROP TABLE public.scene_info;

CREATE TABLE public.scene_info (
	id varchar(36) NOT NULL,
	"name" varchar(255) NOT NULL, -- 名称
	description varchar(255) NULL, -- 描述
	tenant_id varchar(36) NOT NULL, -- 租户ID
	creator varchar(36) NOT NULL, -- 创建人ID
	updator varchar(36) NULL, -- 修改人ID
	created_at timestamptz(6) NOT NULL, -- 创建时间
	updated_at timestamptz(6) NULL, -- 更新时间
	CONSTRAINT scene_info_pkey PRIMARY KEY (id)
);

-- Column comments

COMMENT ON COLUMN public.scene_info."name" IS '名称';
COMMENT ON COLUMN public.scene_info.description IS '描述';
COMMENT ON COLUMN public.scene_info.tenant_id IS '租户ID';
COMMENT ON COLUMN public.scene_info.creator IS '创建人ID';
COMMENT ON COLUMN public.scene_info.updator IS '修改人ID';
COMMENT ON COLUMN public.scene_info.created_at IS '创建时间';
COMMENT ON COLUMN public.scene_info.updated_at IS '更新时间';


-- public.sys_dict definition

-- Drop table

-- DROP TABLE public.sys_dict;

CREATE TABLE public.sys_dict (
	id varchar(36) NOT NULL, -- 主键ID
	dict_code varchar(36) NOT NULL, -- 字典标识符
	dict_value varchar(255) NOT NULL, -- 字典值
	created_at timestamptz(6) NOT NULL, -- 创建时间
	remark varchar(255) NULL, -- 备注
	CONSTRAINT sys_dict_dict_code_dict_value_key UNIQUE (dict_code, dict_value),
	CONSTRAINT sys_dict_pkey PRIMARY KEY (id)
);

-- Column comments

COMMENT ON COLUMN public.sys_dict.id IS '主键ID';
COMMENT ON COLUMN public.sys_dict.dict_code IS '字典标识符';
COMMENT ON COLUMN public.sys_dict.dict_value IS '字典值';
COMMENT ON COLUMN public.sys_dict.created_at IS '创建时间';
COMMENT ON COLUMN public.sys_dict.remark IS '备注';

-- Constraint comments

COMMENT ON CONSTRAINT sys_dict_dict_code_dict_value_key ON public.sys_dict IS 'dict_code和dict_value唯一';


-- public.sys_function definition

-- Drop table

-- DROP TABLE public.sys_function;

CREATE TABLE public.sys_function (
	id varchar(36) NOT NULL, -- id
	"name" varchar(50) NOT NULL, -- 功能名称
	enable_flag varchar(20) NOT NULL, -- 启用标志 enable-启用 disable-禁用
	description varchar(500) NULL, -- 描述
	remark varchar(255) NULL, -- 备注
	CONSTRAINT sys_function_pk PRIMARY KEY (id)
);

-- Column comments

COMMENT ON COLUMN public.sys_function.id IS 'id';
COMMENT ON COLUMN public.sys_function."name" IS '功能名称';
COMMENT ON COLUMN public.sys_function.enable_flag IS '启用标志 enable-启用 disable-禁用';
COMMENT ON COLUMN public.sys_function.description IS '描述';
COMMENT ON COLUMN public.sys_function.remark IS '备注';


-- public.sys_ui_elements definition

-- Drop table

-- DROP TABLE public.sys_ui_elements;

CREATE TABLE public.sys_ui_elements (
	id varchar(36) NOT NULL, -- 主键ID
	parent_id varchar(36) NOT NULL, -- 父元素id
	element_code varchar(100) NOT NULL, -- 元素标识符
	element_type int2 NOT NULL, -- 元素类型1-菜单 2-目录 3-按钮 4-路由
	orders int2 NULL, -- 排序
	param1 varchar(255) NULL,
	param2 varchar(255) NULL,
	param3 varchar(255) NULL,
	authority json NOT NULL, -- 权限(多选)1-系统管理员 2-租户 例如[1,2]
	description varchar(255) NULL, -- 描述
	created_at timestamptz(6) NOT NULL,
	remark varchar(255) NULL,
	multilingual varchar(100) NULL, -- 多语言标识符
	route_path varchar(255) NULL,
	CONSTRAINT sys_ui_elements_pkey PRIMARY KEY (id)
);
COMMENT ON TABLE public.sys_ui_elements IS 'UI元素表';

-- Column comments

COMMENT ON COLUMN public.sys_ui_elements.id IS '主键ID';
COMMENT ON COLUMN public.sys_ui_elements.parent_id IS '父元素id';
COMMENT ON COLUMN public.sys_ui_elements.element_code IS '元素标识符';
COMMENT ON COLUMN public.sys_ui_elements.element_type IS '元素类型1-菜单 2-目录 3-按钮 4-路由';
COMMENT ON COLUMN public.sys_ui_elements.orders IS '排序';
COMMENT ON COLUMN public.sys_ui_elements.authority IS '权限(多选)1-系统管理员 2-租户 例如[1,2]';
COMMENT ON COLUMN public.sys_ui_elements.description IS '描述';
COMMENT ON COLUMN public.sys_ui_elements.multilingual IS '多语言标识符';



-- public.telemetry_current_datas definition

-- Drop table

-- DROP TABLE public.telemetry_current_datas;

CREATE TABLE public.telemetry_current_datas (
	device_id varchar(36) NOT NULL, -- 设备ID
	"key" varchar(255) NOT NULL, -- 数据标识符
	ts timestamptz(6) NOT NULL, -- 上报时间
	bool_v bool NULL,
	number_v float8 NULL,
	string_v text NULL,
	tenant_id varchar(36) NULL,
	CONSTRAINT telemetry_current_datas_unique UNIQUE (device_id, key)
);
CREATE INDEX telemetry_datas_ts_idx_copy1 ON public.telemetry_current_datas USING btree (ts DESC);

-- Column comments

COMMENT ON COLUMN public.telemetry_current_datas.device_id IS '设备ID';
COMMENT ON COLUMN public.telemetry_current_datas."key" IS '数据标识符';
COMMENT ON COLUMN public.telemetry_current_datas.ts IS '上报时间';


-- public.telemetry_datas definition

-- Drop table

-- DROP TABLE public.telemetry_datas;

CREATE TABLE public.telemetry_datas (
	device_id varchar(36) NOT NULL, -- 设备ID
	"key" varchar(255) NOT NULL, -- 数据标识符
	ts int8 NOT NULL, -- 上报时间
	bool_v bool NULL,
	number_v float8 NULL,
	string_v text NULL,
	tenant_id varchar(36) NULL,
	CONSTRAINT telemetry_datas_device_id_key_ts_key UNIQUE (device_id, key, ts)
);
CREATE INDEX telemetry_datas_ts_idx ON public.telemetry_datas USING btree (ts DESC);

-- Column comments

COMMENT ON COLUMN public.telemetry_datas.device_id IS '设备ID';
COMMENT ON COLUMN public.telemetry_datas."key" IS '数据标识符';
COMMENT ON COLUMN public.telemetry_datas.ts IS '上报时间';

-- Table Triggers

--24小时分区
SELECT create_hypertable('telemetry_datas', 'ts',chunk_time_interval => 86400000000);

-- public.users definition

-- Drop table

-- DROP TABLE public.users;

CREATE TABLE public.users (
	id varchar(36) NOT NULL,
	"name" varchar(255) NULL,
	phone_number varchar(50) NOT NULL,
	email varchar(255) NOT NULL,
	status varchar(2) NULL, -- 用户状态 F-冻结 N-正常
	authority varchar(50) NULL, -- 权限类型 TENANT_ADMIN-租户管理员 TENANT_USER-租户用户 SYS_ADMIN-系统管理员
	"password" varchar(255) NOT NULL,
	tenant_id varchar(36) NULL,
	remark varchar(255) NULL,
	additional_info json NULL DEFAULT '{}'::json,
	created_at timestamptz(6) NULL,
	updated_at timestamptz(6) NULL,
	CONSTRAINT users_pkey PRIMARY KEY (id),
	CONSTRAINT users_un UNIQUE (email)
);
COMMENT ON TABLE public.users IS '用户';

-- Column comments

COMMENT ON COLUMN public.users.status IS '用户状态 F-冻结 N-正常';
COMMENT ON COLUMN public.users.authority IS '权限类型 TENANT_ADMIN-租户管理员 TENANT_USER-租户用户 SYS_ADMIN-系统管理员';


-- public.vis_dashboard definition

-- Drop table

-- DROP TABLE public.vis_dashboard;

CREATE TABLE public.vis_dashboard (
	id varchar(36) NOT NULL,
	relation_id varchar(36) NULL,
	json_data json NULL DEFAULT '{}'::json,
	dashboard_name varchar(99) NULL,
	create_at timestamp NULL,
	sort int4 NULL, -- 排序
	remark varchar(255) NULL,
	tenant_id varchar(36) NULL,
	share_id varchar(36) NULL, -- 分享id
	CONSTRAINT vis_dashboard_pk PRIMARY KEY (id)
);
COMMENT ON TABLE public.vis_dashboard IS '可视化插件';

-- Column comments

COMMENT ON COLUMN public.vis_dashboard.sort IS '排序';
COMMENT ON COLUMN public.vis_dashboard.share_id IS '分享id';


-- public.vis_files definition

-- Drop table

-- DROP TABLE public.vis_files;

CREATE TABLE public.vis_files (
	id varchar(36) NOT NULL,
	vis_plugin_id varchar(36) NOT NULL, -- 可视化插件id
	file_name varchar(150) NULL, -- 名称
	file_url varchar(150) NULL, -- url地址
	file_size varchar(20) NULL, -- 文件大小
	create_at int8 NULL,
	remark varchar(255) NULL,
	CONSTRAINT tp_vis_files_pkey PRIMARY KEY (id)
);
COMMENT ON TABLE public.vis_files IS '可视化文件表';

-- Column comments

COMMENT ON COLUMN public.vis_files.vis_plugin_id IS '可视化插件id';
COMMENT ON COLUMN public.vis_files.file_name IS '名称';
COMMENT ON COLUMN public.vis_files.file_url IS 'url地址';
COMMENT ON COLUMN public.vis_files.file_size IS '文件大小';


-- public.vis_plugin definition

-- Drop table

-- DROP TABLE public.vis_plugin;

CREATE TABLE public.vis_plugin (
	id varchar(36) NOT NULL,
	tenant_id varchar(36) NOT NULL, -- 租户id
	plugin_name varchar(150) NOT NULL, -- 可视化插件名称
	plugin_description varchar(150) NULL, -- 插件描述
	create_at int8 NULL,
	remark varchar(255) NULL,
	CONSTRAINT tp_vis_plugin_pkey PRIMARY KEY (id)
);
COMMENT ON TABLE public.vis_plugin IS '可视化插件表';

-- Column comments

COMMENT ON COLUMN public.vis_plugin.tenant_id IS '租户id';
COMMENT ON COLUMN public.vis_plugin.plugin_name IS '可视化插件名称';
COMMENT ON COLUMN public.vis_plugin.plugin_description IS '插件描述';


-- public.action_info definition

-- Drop table

-- DROP TABLE public.action_info;

CREATE TABLE public.action_info (
	id varchar(36) NOT NULL,
	scene_automation_id varchar(36) NOT NULL, -- 场景联动ID（外键-关联删除）
	action_target varchar(255) NULL, -- 动作目标id设备id、场景id、告警id；如果条件是单类设备，这里为空
	action_type varchar(10) NOT NULL, -- 动作类型10: 单个设备11: 单类设备20: 激活场景30: 触发告警40: 服务
	action_param_type varchar(10) NULL, -- 遥测TEL属性ATTR命令CMD
	action_param varchar(50) NULL, -- 动作参数动作类型为10,11是有效 标识符
	action_value text NULL, -- 目标值
	remark varchar(255) NULL,
	CONSTRAINT action_info_pkey PRIMARY KEY (id),
	CONSTRAINT action_info_scene_automations_fk FOREIGN KEY (scene_automation_id) REFERENCES public.scene_automations(id) ON DELETE CASCADE
);

-- Column comments

COMMENT ON COLUMN public.action_info.scene_automation_id IS '场景联动ID（外键-关联删除）';
COMMENT ON COLUMN public.action_info.action_target IS '动作目标id设备id、场景id、告警id；如果条件是单类设备，这里为空';
COMMENT ON COLUMN public.action_info.action_type IS '动作类型10: 单个设备11: 单类设备20: 激活场景30: 触发告警40: 服务';
COMMENT ON COLUMN public.action_info.action_param_type IS '遥测TEL属性ATTR命令CMD';
COMMENT ON COLUMN public.action_info.action_param IS '动作参数动作类型为10,11是有效 标识符';
COMMENT ON COLUMN public.action_info.action_value IS '目标值';


-- public.alarm_info definition

-- Drop table

-- DROP TABLE public.alarm_info;

CREATE TABLE public.alarm_info (
	id varchar(36) NOT NULL,
	alarm_config_id varchar(36) NOT NULL, -- 告警配置id
	"name" varchar(255) NOT NULL, -- 告警名称
	alarm_time timestamptz(6) NOT NULL, -- 告警时间
	description varchar(255) NULL, -- 告警描述
	"content" text NULL, -- 内容
	processor varchar(36) NULL, -- 处理人id
	processing_result varchar(10) NOT NULL, -- 处理结果DOP-已处理UND-未处理IGN-已忽略
	tenant_id varchar(36) NOT NULL, -- 租户id
	remark varchar(255) NULL,
	alarm_level varchar(10) NULL, -- 告警级别L M H
	CONSTRAINT alarm_info_pk PRIMARY KEY (id),
	CONSTRAINT alarm_info_fk FOREIGN KEY (alarm_config_id) REFERENCES public.alarm_config(id) ON DELETE CASCADE
);
COMMENT ON TABLE public.alarm_info IS '告警信息';

-- Column comments

COMMENT ON COLUMN public.alarm_info.alarm_config_id IS '告警配置id';
COMMENT ON COLUMN public.alarm_info."name" IS '告警名称';
COMMENT ON COLUMN public.alarm_info.alarm_time IS '告警时间';
COMMENT ON COLUMN public.alarm_info.description IS '告警描述';
COMMENT ON COLUMN public.alarm_info."content" IS '内容';
COMMENT ON COLUMN public.alarm_info.processor IS '处理人id';
COMMENT ON COLUMN public.alarm_info.processing_result IS '处理结果DOP-已处理UND-未处理IGN-已忽略';
COMMENT ON COLUMN public.alarm_info.tenant_id IS '租户id';
COMMENT ON COLUMN public.alarm_info.alarm_level IS '告警级别L M H';


-- public.device_configs definition

-- Drop table

-- DROP TABLE public.device_configs;

CREATE TABLE public.device_configs (
	id varchar(36) NOT NULL, -- Id
	"name" varchar(99) NOT NULL, -- 名称
	device_template_id varchar(36) NULL, -- 设备模板id
	device_type varchar(9) NOT NULL, -- 设备类型 1直连设备 2网关设备 3网关子设备
	protocol_type varchar(36) NULL, -- 协议类型
	voucher_type varchar(36) NULL, -- 凭证类型
	protocol_config json NULL, -- 协议表单配置
	device_conn_type varchar(36) NULL, -- 设备连接方式（默认A）A-设备连接平台B-平台连接设备
	additional_info json NULL DEFAULT '{}'::json, -- 附加信息
	description varchar(255) NULL, -- 描述
	tenant_id varchar(36) NOT NULL, -- 租户id
	created_at timestamptz(6) NOT NULL, -- 创建时间
	updated_at timestamptz(6) NOT NULL, -- 更新时间
	remark varchar(255) NULL, -- 备注
	other_config json NULL, -- 其他配置
	CONSTRAINT device_configs_pkey PRIMARY KEY (id),
	CONSTRAINT device_configs_device_templates_fk FOREIGN KEY (device_template_id) REFERENCES public.device_templates(id) ON DELETE RESTRICT
);

-- Column comments

COMMENT ON COLUMN public.device_configs.id IS 'Id';
COMMENT ON COLUMN public.device_configs."name" IS '名称';
COMMENT ON COLUMN public.device_configs.device_template_id IS '设备模板id';
COMMENT ON COLUMN public.device_configs.device_type IS '设备类型 1直连设备 2网关设备 3网关子设备';
COMMENT ON COLUMN public.device_configs.protocol_type IS '协议类型';
COMMENT ON COLUMN public.device_configs.voucher_type IS '凭证类型';
COMMENT ON COLUMN public.device_configs.protocol_config IS '协议表单配置';
COMMENT ON COLUMN public.device_configs.device_conn_type IS '设备连接方式（默认A）A-设备连接平台B-平台连接设备';
COMMENT ON COLUMN public.device_configs.additional_info IS '附加信息';
COMMENT ON COLUMN public.device_configs.description IS '描述';
COMMENT ON COLUMN public.device_configs.tenant_id IS '租户id';
COMMENT ON COLUMN public.device_configs.created_at IS '创建时间';
COMMENT ON COLUMN public.device_configs.updated_at IS '更新时间';
COMMENT ON COLUMN public.device_configs.remark IS '备注';
COMMENT ON COLUMN public.device_configs.other_config IS '其他配置';


-- public.device_model_attributes definition

-- Drop table

-- DROP TABLE public.device_model_attributes;

CREATE TABLE public.device_model_attributes (
	id varchar(36) NOT NULL, -- id
	device_template_id varchar(36) NOT NULL, -- 设备模板id
	data_name varchar(255) NULL, -- 数据名称
	data_identifier varchar(255) NOT NULL, -- 数据标识符
	read_write_flag varchar(10) NULL, -- 读写标志R-读 W-写 RW-读写
	data_type varchar(50) NULL, -- 数据类型String Number Boolean Enum
	unit varchar(50) NULL, -- 单位
	description varchar(255) NULL, -- 描述
	additional_info json NULL, -- 附加信息
	created_at timestamptz(6) NOT NULL, -- 创建时间
	updated_at timestamptz(6) NOT NULL, -- 更新时间
	remark varchar(255) NULL, -- 备注
	tenant_id varchar(36) NOT NULL,
	CONSTRAINT device_model_attributes_unique UNIQUE (device_template_id, data_identifier),
	CONSTRAINT device_model_telemetry_copy1_pkey PRIMARY KEY (id),
	CONSTRAINT device_model_attributes_device_templates_fk FOREIGN KEY (device_template_id) REFERENCES public.device_templates(id) ON DELETE CASCADE
);

-- Column comments

COMMENT ON COLUMN public.device_model_attributes.id IS 'id';
COMMENT ON COLUMN public.device_model_attributes.device_template_id IS '设备模板id';
COMMENT ON COLUMN public.device_model_attributes.data_name IS '数据名称';
COMMENT ON COLUMN public.device_model_attributes.data_identifier IS '数据标识符';
COMMENT ON COLUMN public.device_model_attributes.read_write_flag IS '读写标志R-读 W-写 RW-读写';
COMMENT ON COLUMN public.device_model_attributes.data_type IS '数据类型String Number Boolean Enum';
COMMENT ON COLUMN public.device_model_attributes.unit IS '单位';
COMMENT ON COLUMN public.device_model_attributes.description IS '描述';
COMMENT ON COLUMN public.device_model_attributes.additional_info IS '附加信息';
COMMENT ON COLUMN public.device_model_attributes.created_at IS '创建时间';
COMMENT ON COLUMN public.device_model_attributes.updated_at IS '更新时间';
COMMENT ON COLUMN public.device_model_attributes.remark IS '备注';


-- public.device_model_commands definition

-- Drop table

-- DROP TABLE public.device_model_commands;

CREATE TABLE public.device_model_commands (
	id varchar(36) NOT NULL, -- id
	device_template_id varchar(36) NOT NULL, -- 设备模板id
	data_name varchar(255) NULL, -- 数据名称
	data_identifier varchar(255) NOT NULL, -- 数据标识符
	params json NULL, -- 参数
	description varchar(255) NULL, -- 描述
	additional_info json NULL, -- 附加信息
	created_at timestamptz(6) NOT NULL, -- 创建时间
	updated_at timestamptz(6) NOT NULL, -- 更新时间
	remark varchar(255) NULL, -- 备注
	tenant_id varchar(36) NOT NULL,
	CONSTRAINT device_model_commands_unique UNIQUE (data_identifier, device_template_id),
	CONSTRAINT device_model_telemetry_copy1_pkey2 PRIMARY KEY (id),
	CONSTRAINT device_model_commands_device_templates_fk FOREIGN KEY (device_template_id) REFERENCES public.device_templates(id) ON DELETE CASCADE
);

-- Column comments

COMMENT ON COLUMN public.device_model_commands.id IS 'id';
COMMENT ON COLUMN public.device_model_commands.device_template_id IS '设备模板id';
COMMENT ON COLUMN public.device_model_commands.data_name IS '数据名称';
COMMENT ON COLUMN public.device_model_commands.data_identifier IS '数据标识符';
COMMENT ON COLUMN public.device_model_commands.params IS '参数';
COMMENT ON COLUMN public.device_model_commands.description IS '描述';
COMMENT ON COLUMN public.device_model_commands.additional_info IS '附加信息';
COMMENT ON COLUMN public.device_model_commands.created_at IS '创建时间';
COMMENT ON COLUMN public.device_model_commands.updated_at IS '更新时间';
COMMENT ON COLUMN public.device_model_commands.remark IS '备注';


-- public.device_model_events definition

-- Drop table

-- DROP TABLE public.device_model_events;

CREATE TABLE public.device_model_events (
	id varchar(36) NOT NULL, -- id
	device_template_id varchar(36) NOT NULL, -- 设备模板id
	data_name varchar(255) NULL, -- 数据名称
	data_identifier varchar(255) NOT NULL, -- 数据标识符
	params json NULL, -- 参数
	description varchar(255) NULL, -- 描述
	additional_info json NULL, -- 附加信息
	created_at timestamptz(6) NOT NULL, -- 创建时间
	updated_at timestamptz(6) NOT NULL, -- 更新时间
	remark varchar(255) NULL, -- 备注
	tenant_id varchar(36) NOT NULL,
	CONSTRAINT device_model_events_unique UNIQUE (device_template_id, data_identifier),
	CONSTRAINT device_model_telemetry_copy1_pkey1 PRIMARY KEY (id),
	CONSTRAINT device_model_events_device_templates_fk FOREIGN KEY (device_template_id) REFERENCES public.device_templates(id) ON DELETE CASCADE
);

-- Column comments

COMMENT ON COLUMN public.device_model_events.id IS 'id';
COMMENT ON COLUMN public.device_model_events.device_template_id IS '设备模板id';
COMMENT ON COLUMN public.device_model_events.data_name IS '数据名称';
COMMENT ON COLUMN public.device_model_events.data_identifier IS '数据标识符';
COMMENT ON COLUMN public.device_model_events.params IS '参数';
COMMENT ON COLUMN public.device_model_events.description IS '描述';
COMMENT ON COLUMN public.device_model_events.additional_info IS '附加信息';
COMMENT ON COLUMN public.device_model_events.created_at IS '创建时间';
COMMENT ON COLUMN public.device_model_events.updated_at IS '更新时间';
COMMENT ON COLUMN public.device_model_events.remark IS '备注';


-- public.device_model_telemetry definition

-- Drop table

-- DROP TABLE public.device_model_telemetry;

CREATE TABLE public.device_model_telemetry (
	id varchar(36) NOT NULL, -- id
	device_template_id varchar(36) NOT NULL, -- 设备模板id
	data_name varchar(255) NULL, -- 数据名称
	data_identifier varchar(255) NOT NULL, -- 数据标识符
	read_write_flag varchar(10) NULL, -- 读写标志R-读 W-写 RW-读写
	data_type varchar(50) NULL, -- 数据类型String Number Boolean
	unit varchar(50) NULL, -- 单位
	description varchar(255) NULL, -- 描述
	additional_info json NULL, -- 附加信息
	created_at timestamptz(6) NOT NULL, -- 创建时间
	updated_at timestamptz(6) NOT NULL, -- 更新时间
	remark varchar(255) NULL, -- 备注
	tenant_id varchar(36) NOT NULL,
	CONSTRAINT device_model_telemetry_pkey PRIMARY KEY (id),
	CONSTRAINT device_model_telemetry_unique UNIQUE (device_template_id, data_identifier),
	CONSTRAINT device_model_telemetry_device_templates_fk FOREIGN KEY (device_template_id) REFERENCES public.device_templates(id) ON DELETE CASCADE
);

-- Column comments

COMMENT ON COLUMN public.device_model_telemetry.id IS 'id';
COMMENT ON COLUMN public.device_model_telemetry.device_template_id IS '设备模板id';
COMMENT ON COLUMN public.device_model_telemetry.data_name IS '数据名称';
COMMENT ON COLUMN public.device_model_telemetry.data_identifier IS '数据标识符';
COMMENT ON COLUMN public.device_model_telemetry.read_write_flag IS '读写标志R-读 W-写 RW-读写';
COMMENT ON COLUMN public.device_model_telemetry.data_type IS '数据类型String Number Boolean';
COMMENT ON COLUMN public.device_model_telemetry.unit IS '单位';
COMMENT ON COLUMN public.device_model_telemetry.description IS '描述';
COMMENT ON COLUMN public.device_model_telemetry.additional_info IS '附加信息';
COMMENT ON COLUMN public.device_model_telemetry.created_at IS '创建时间';
COMMENT ON COLUMN public.device_model_telemetry.updated_at IS '更新时间';
COMMENT ON COLUMN public.device_model_telemetry.remark IS '备注';


-- public.device_trigger_condition definition

-- Drop table

-- DROP TABLE public.device_trigger_condition;

CREATE TABLE public.device_trigger_condition (
	id varchar(36) NOT NULL, -- Id
	scene_automation_id varchar(36) NOT NULL, -- 场景联动ID（外键-关联删除）
	enabled varchar(10) NOT NULL, -- 是否启用
	group_id varchar(36) NOT NULL, -- uuid
	trigger_condition_type varchar(10) NOT NULL, -- 条件类型 10：设备类型 - 单个设备 11：设备类型 - 单类设备 2：时间范围
	trigger_source varchar(36) NULL, -- 触发源有以下几种可能： 条件类型为10时，为设备id；条件类型为11时，设备配置id（device_config_id）
	trigger_param_type varchar(10) NULL, -- 遥测TEL属性ATTR事件EVT状态STATUS
	trigger_param varchar(50) NULL, -- 触发参数 当条件类型为10或11时有效，比如温度 temperature
	trigger_operator varchar(10) NULL, -- 运算符 =：等于 !=：不等于 >：大于 <：小于 >=：大于等于 <=：小于等于 between：介于 in：包含在列表内
	trigger_value varchar(99) NOT NULL, -- 取值条件类型为10,11，运算符是为7时，假设最大值6最小值2, 格式为2-6；设备状态条件类型为10,11，运算符为8时，多个值英文逗号隔开条件类型为 条件类型是22，示例137|HH:mm:ss+00:00|HH:mm:ss+00:00
	remark varchar(255) NULL,
	tenant_id varchar(36) NOT NULL, -- 租户ID
	CONSTRAINT device_trigger_condition_pkey PRIMARY KEY (id),
	CONSTRAINT fk_scene_automation_id FOREIGN KEY (scene_automation_id) REFERENCES public.scene_automations(id) ON DELETE CASCADE
);

-- Column comments

COMMENT ON COLUMN public.device_trigger_condition.id IS 'Id';
COMMENT ON COLUMN public.device_trigger_condition.scene_automation_id IS '场景联动ID（外键-关联删除）';
COMMENT ON COLUMN public.device_trigger_condition.enabled IS '是否启用';
COMMENT ON COLUMN public.device_trigger_condition.group_id IS 'uuid';
COMMENT ON COLUMN public.device_trigger_condition.trigger_condition_type IS '条件类型 10：设备类型 - 单个设备 11：设备类型 - 单类设备 2：时间范围';
COMMENT ON COLUMN public.device_trigger_condition.trigger_source IS '触发源有以下几种可能： 条件类型为10时，为设备id；条件类型为11时，设备配置id（device_config_id）';
COMMENT ON COLUMN public.device_trigger_condition.trigger_param_type IS '遥测TEL属性ATTR事件EVT状态STATUS';
COMMENT ON COLUMN public.device_trigger_condition.trigger_param IS '触发参数 当条件类型为10或11时有效，比如温度 temperature';
COMMENT ON COLUMN public.device_trigger_condition.trigger_operator IS '运算符 =：等于 !=：不等于 >：大于 <：小于 >=：大于等于 <=：小于等于 between：介于 in：包含在列表内';
COMMENT ON COLUMN public.device_trigger_condition.trigger_value IS '取值条件类型为10,11，运算符是为7时，假设最大值6最小值2, 格式为2-6；设备状态条件类型为10,11，运算符为8时，多个值英文逗号隔开条件类型为 条件类型是22，示例137|HH:mm:ss+00:00|HH:mm:ss+00:00';
COMMENT ON COLUMN public.device_trigger_condition.tenant_id IS '租户ID';


-- public.one_time_tasks definition

-- Drop table

-- DROP TABLE public.one_time_tasks;

CREATE TABLE public.one_time_tasks (
	id varchar(36) NOT NULL,
	scene_automation_id varchar(36) NOT NULL, -- 场景联动ID（外键-关联删除）
	execution_time timestamptz(6) NOT NULL, -- 执行时间
	executing_state varchar(10) NOT NULL, -- 1.执行状态 NEX-未执行 EXE-已执行 EXP-过期未执行
	enabled varchar(10) NOT NULL, -- 是否启用 Y-启用 N-停用
	remark varchar(255) NULL,
	expiration_time int8 NOT NULL, -- 过期时间（默认大于执行时间五分钟5min10min30min1h1day）单位分钟
	CONSTRAINT one_time_tasks_pkey PRIMARY KEY (id),
	CONSTRAINT fk_scene_automation_id FOREIGN KEY (scene_automation_id) REFERENCES public.scene_automations(id) ON DELETE CASCADE
);

-- Column comments

COMMENT ON COLUMN public.one_time_tasks.scene_automation_id IS '场景联动ID（外键-关联删除）';
COMMENT ON COLUMN public.one_time_tasks.execution_time IS '执行时间';
COMMENT ON COLUMN public.one_time_tasks.executing_state IS '1.执行状态 NEX-未执行 EXE-已执行 EXP-过期未执行';
COMMENT ON COLUMN public.one_time_tasks.enabled IS '是否启用 Y-启用 N-停用';
COMMENT ON COLUMN public.one_time_tasks.expiration_time IS '过期时间（默认大于执行时间五分钟5min10min30min1h1day）单位分钟';


-- public.periodic_tasks definition

-- Drop table

-- DROP TABLE public.periodic_tasks;

CREATE TABLE public.periodic_tasks (
	id varchar(36) NOT NULL,
	scene_automation_id varchar(36) NOT NULL, -- 场景联动ID（外键-关联删除）
	task_type varchar(255) NOT NULL, -- 任务类型 HOUR DAY WEEK MONTH CRON
	params varchar(50) NOT NULL,
	execution_time timestamptz(6) NOT NULL, -- 执行时间
	enabled varchar(10) NOT NULL, -- 是否启用 Y-启用 N-停用
	remark varchar(255) NULL,
	expiration_time int8 NOT NULL, -- 过期时间（默认大于执行时间五分钟）单位分钟
	CONSTRAINT periodic_tasks_pkey PRIMARY KEY (id),
	CONSTRAINT scene_automation_id_fkey FOREIGN KEY (scene_automation_id) REFERENCES public.scene_automations(id) ON DELETE CASCADE
);

-- Column comments

COMMENT ON COLUMN public.periodic_tasks.scene_automation_id IS '场景联动ID（外键-关联删除）';
COMMENT ON COLUMN public.periodic_tasks.task_type IS '任务类型 HOUR DAY WEEK MONTH CRON';
COMMENT ON COLUMN public.periodic_tasks.execution_time IS '执行时间';
COMMENT ON COLUMN public.periodic_tasks.enabled IS '是否启用 Y-启用 N-停用';
COMMENT ON COLUMN public.periodic_tasks.expiration_time IS '过期时间（默认大于执行时间五分钟）单位分钟';


-- public.products definition

-- Drop table

-- DROP TABLE public.products;

CREATE TABLE public.products (
	id varchar(36) NOT NULL, -- uuid
	"name" varchar(255) NOT NULL, -- 产品名称
	description varchar(255) NULL, -- 描述
	product_type varchar(36) NULL, -- 产品类型
	product_key varchar(255) NULL, -- 产品key
	product_model varchar(100) NULL, -- 产品型号(编号)
	image_url varchar(500) NULL, -- 图片
	created_at timestamptz(6) NOT NULL, -- 创建时间
	remark varchar(500) NULL,
	additional_info json NULL,
	tenant_id varchar(36) NULL, -- 租户id
	device_config_id varchar(36) NULL,
	CONSTRAINT products_pkey PRIMARY KEY (id),
	CONSTRAINT products_device_configs_fk FOREIGN KEY (device_config_id) REFERENCES public.device_configs(id) ON DELETE RESTRICT
);

-- Column comments

COMMENT ON COLUMN public.products.id IS 'uuid';
COMMENT ON COLUMN public.products."name" IS '产品名称';
COMMENT ON COLUMN public.products.description IS '描述';
COMMENT ON COLUMN public.products.product_type IS '产品类型';
COMMENT ON COLUMN public.products.product_key IS '产品key';
COMMENT ON COLUMN public.products.product_model IS '产品型号(编号)';
COMMENT ON COLUMN public.products.image_url IS '图片';
COMMENT ON COLUMN public.products.created_at IS '创建时间';
COMMENT ON COLUMN public.products.tenant_id IS '租户id';


-- public.scene_action_info definition

-- Drop table

-- DROP TABLE public.scene_action_info;

CREATE TABLE public.scene_action_info (
	id varchar(36) NOT NULL,
	scene_id varchar(36) NOT NULL, -- 场景id（关联删除）
	action_target varchar(36) NOT NULL, -- 动作目标id设备id、设备配置id，场景id、告警id
	action_type varchar(10) NOT NULL, -- 动作类型10: 单个设备11: 单类设备20: 激活场景30: 触发告警40: 服务
	action_param_type varchar(10) NULL, -- 1.参数类型TEL:遥测 2.ATTR:属性 CMD:命令
	action_param varchar(10) NULL, -- 动作参数
	action_value varchar(255) NULL, -- 目标值
	created_at timestamptz(6) NOT NULL, -- 创建时间
	updated_at timestamptz(6) NULL, -- 更新时间
	tenant_id varchar(36) NOT NULL,
	remark varchar(255) NULL,
	CONSTRAINT scene_action_info_pkey PRIMARY KEY (id),
	CONSTRAINT scene_action_info_scene_id_fkey FOREIGN KEY (scene_id) REFERENCES public.scene_info(id) ON DELETE CASCADE
);

-- Column comments

COMMENT ON COLUMN public.scene_action_info.scene_id IS '场景id（关联删除）';
COMMENT ON COLUMN public.scene_action_info.action_target IS '动作目标id设备id、设备配置id，场景id、告警id';
COMMENT ON COLUMN public.scene_action_info.action_type IS '动作类型10: 单个设备11: 单类设备20: 激活场景30: 触发告警40: 服务';
COMMENT ON COLUMN public.scene_action_info.action_param_type IS '1.参数类型TEL:遥测 2.ATTR:属性 CMD:命令';
COMMENT ON COLUMN public.scene_action_info.action_param IS '动作参数';
COMMENT ON COLUMN public.scene_action_info.action_value IS '目标值';
COMMENT ON COLUMN public.scene_action_info.created_at IS '创建时间';
COMMENT ON COLUMN public.scene_action_info.updated_at IS '更新时间';


-- public.scene_automation_log definition

-- Drop table

-- DROP TABLE public.scene_automation_log;

CREATE TABLE public.scene_automation_log (
	scene_automation_id varchar(36) NOT NULL, -- 场景联动ID（外键-关联删除）
	executed_at timestamptz(6) NOT NULL, -- 执行时间
	detail text NOT NULL, -- 执行说明：详细的执行过程
	execution_result varchar(10) NOT NULL, -- 执行状态S：成功F：失败 全部执行成功才算
	tenant_id varchar(36) NOT NULL,
	remark varchar(255) NULL,
	CONSTRAINT scene_automation_log_scene_automation_id_fkey FOREIGN KEY (scene_automation_id) REFERENCES public.scene_automations(id) ON DELETE RESTRICT
);

-- Column comments

COMMENT ON COLUMN public.scene_automation_log.scene_automation_id IS '场景联动ID（外键-关联删除）';
COMMENT ON COLUMN public.scene_automation_log.executed_at IS '执行时间';
COMMENT ON COLUMN public.scene_automation_log.detail IS '执行说明：详细的执行过程';
COMMENT ON COLUMN public.scene_automation_log.execution_result IS '执行状态S：成功F：失败 全部执行成功才算';


-- public.scene_log definition

-- Drop table

-- DROP TABLE public.scene_log;

CREATE TABLE public.scene_log (
	scene_id varchar(36) NOT NULL, -- 场景id（关联删除）
	executed_at timestamptz(6) NOT NULL, -- 执行时间
	detail text NOT NULL, -- 执行说明：详细的执行过程
	execution_result varchar(10) NOT NULL, -- 执行状态S：成功F：失败 全部执行成功才算成功
	tenant_id varchar(36) NOT NULL,
	remark varchar(255) NULL,
	id varchar(36) NOT NULL,
	CONSTRAINT scene_log_pkey PRIMARY KEY (id),
	CONSTRAINT scene_log_scene_id_fkey FOREIGN KEY (scene_id) REFERENCES public.scene_info(id) ON DELETE CASCADE
);

-- Column comments

COMMENT ON COLUMN public.scene_log.scene_id IS '场景id（关联删除）';
COMMENT ON COLUMN public.scene_log.executed_at IS '执行时间';
COMMENT ON COLUMN public.scene_log.detail IS '执行说明：详细的执行过程';
COMMENT ON COLUMN public.scene_log.execution_result IS '执行状态S：成功F：失败 全部执行成功才算成功';


-- public.sys_dict_language definition

-- Drop table

-- DROP TABLE public.sys_dict_language;

CREATE TABLE public.sys_dict_language (
	id varchar(36) NOT NULL, -- 主键ID
	dict_id varchar(36) NOT NULL, -- sys_dict.id
	language_code varchar(36) NOT NULL, -- 语言代码
	"translation" varchar(255) NOT NULL, -- 翻译
	CONSTRAINT sys_dict_language_dict_id_language_code_key UNIQUE (dict_id, language_code),
	CONSTRAINT sys_dict_language_pkey PRIMARY KEY (id),
	CONSTRAINT sys_dict_language_dict_id_fkey FOREIGN KEY (dict_id) REFERENCES public.sys_dict(id) ON DELETE CASCADE
);

-- Column comments

COMMENT ON COLUMN public.sys_dict_language.id IS '主键ID';
COMMENT ON COLUMN public.sys_dict_language.dict_id IS 'sys_dict.id';
COMMENT ON COLUMN public.sys_dict_language.language_code IS '语言代码';
COMMENT ON COLUMN public.sys_dict_language."translation" IS '翻译';

-- Constraint comments

COMMENT ON CONSTRAINT sys_dict_language_dict_id_language_code_key ON public.sys_dict_language IS 'dict_id和language_code唯一';


-- public.data_scripts definition

-- Drop table

-- DROP TABLE public.data_scripts;

CREATE TABLE public.data_scripts (
	id varchar(36) NOT NULL, -- Id
	"name" varchar(99) NOT NULL, -- 名称
	device_config_id varchar(36) NOT NULL, -- 设备配置id 关联删除
	enable_flag varchar(9) NOT NULL, -- 启用标志Y-启用 N-停用 默认启用
	"content" text NULL, -- 内容
	script_type varchar(9) NOT NULL, -- 脚本类型 A-遥测上报预处理B-遥测下发预处理C-属性上报预处理D-属性下发预处理
	last_analog_input text NULL, -- 上次模拟输入
	description varchar(255) NULL, -- 描述
	created_at timestamptz(6) NULL, -- 创建时间
	updated_at timestamptz(6) NULL, -- 更新时间
	remark varchar(255) NULL, -- 备注
	CONSTRAINT data_scripts_pkey PRIMARY KEY (id),
	CONSTRAINT data_scripts_device_configs_fk FOREIGN KEY (device_config_id) REFERENCES public.device_configs(id) ON DELETE CASCADE
);

-- Column comments

COMMENT ON COLUMN public.data_scripts.id IS 'Id';
COMMENT ON COLUMN public.data_scripts."name" IS '名称';
COMMENT ON COLUMN public.data_scripts.device_config_id IS '设备配置id 关联删除';
COMMENT ON COLUMN public.data_scripts.enable_flag IS '启用标志Y-启用 N-停用 默认启用';
COMMENT ON COLUMN public.data_scripts."content" IS '内容';
COMMENT ON COLUMN public.data_scripts.script_type IS '脚本类型 A-遥测上报预处理B-遥测下发预处理C-属性上报预处理D-属性下发预处理';
COMMENT ON COLUMN public.data_scripts.last_analog_input IS '上次模拟输入';
COMMENT ON COLUMN public.data_scripts.description IS '描述';
COMMENT ON COLUMN public.data_scripts.created_at IS '创建时间';
COMMENT ON COLUMN public.data_scripts.updated_at IS '更新时间';
COMMENT ON COLUMN public.data_scripts.remark IS '备注';


-- public.devices definition

-- Drop table

-- DROP TABLE public.devices;

CREATE TABLE public.devices (
	id varchar(36) NOT NULL, -- Id
	"name" varchar(255) NULL, -- 设备名称
	voucher varchar(500) NOT NULL DEFAULT ''::character varying, -- 凭证
	tenant_id varchar(36) NOT NULL DEFAULT ''::character varying, -- 租户id，外键，删除时阻止
	is_enabled varchar(36) NOT NULL DEFAULT ''::character varying, -- 启用/禁用 enabled-启用 disabled-禁用 默认禁用，激活后默认启用
	activate_flag varchar(36) NOT NULL DEFAULT ''::character varying, -- 激活标志inactive-未激活 active-已激活
	created_at timestamptz(6) NULL, -- 创建时间
	update_at timestamptz(6) NULL, -- 更新时间
	device_number varchar(36) NOT NULL DEFAULT ''::character varying, -- 设备编号 没送默认和token一样
	product_id varchar(36) NULL, -- 产品id 外键，删除时阻止
	parent_id varchar(36) NULL, -- 子设备的网关id
	protocol varchar(36) NULL, -- 通讯协议
	"label" varchar(255) NULL, -- 标签 单标签，英文逗号隔开
	"location" varchar(100) NULL, -- 地理位置
	sub_device_addr varchar(36) NULL, -- 子设备地址
	current_version varchar(36) NULL, -- 当前固件版本
	additional_info json NULL DEFAULT '{}'::json, -- 其他信息 阈值、图片等
	protocol_config json NULL DEFAULT '{}'::json, -- 协议表单配置
	remark1 varchar(255) NULL,
	remark2 varchar(255) NULL,
	remark3 varchar(255) NULL,
	device_config_id varchar(36) NULL, -- 设备配置id（外键）¶
	batch_number varchar(500) NULL, -- 批次编号¶
	activate_at timestamptz(6) NULL, -- 激活日期
	is_online int2 NOT NULL DEFAULT 0, -- 是否在线 1-在线 0-离线
	access_way varchar(10) NULL, -- 接入方式A-通过协议 B通过服务
	description varchar(500) NULL, -- 描述
	CONSTRAINT device_pkey PRIMARY KEY (id),
	CONSTRAINT devices_unique UNIQUE (device_number),
	CONSTRAINT devices_unique_1 UNIQUE (voucher),
	CONSTRAINT fk_device_config_id FOREIGN KEY (device_config_id) REFERENCES public.device_configs(id) ON DELETE RESTRICT,
	CONSTRAINT fk_product_id FOREIGN KEY (product_id) REFERENCES public.products(id) ON DELETE RESTRICT
);

-- Column comments

COMMENT ON COLUMN public.devices.id IS 'Id';
COMMENT ON COLUMN public.devices."name" IS '设备名称';
COMMENT ON COLUMN public.devices.voucher IS '凭证';
COMMENT ON COLUMN public.devices.tenant_id IS '租户id，外键，删除时阻止';
COMMENT ON COLUMN public.devices.is_enabled IS '启用/禁用 enabled-启用 disabled-禁用 默认禁用，激活后默认启用';
COMMENT ON COLUMN public.devices.activate_flag IS '激活标志inactive-未激活 active-已激活';
COMMENT ON COLUMN public.devices.created_at IS '创建时间';
COMMENT ON COLUMN public.devices.update_at IS '更新时间';
COMMENT ON COLUMN public.devices.device_number IS '设备编号 没送默认和token一样';
COMMENT ON COLUMN public.devices.product_id IS '产品id 外键，删除时阻止';
COMMENT ON COLUMN public.devices.parent_id IS '子设备的网关id';
COMMENT ON COLUMN public.devices.protocol IS '通讯协议';
COMMENT ON COLUMN public.devices."label" IS '标签 单标签，英文逗号隔开';
COMMENT ON COLUMN public.devices."location" IS '地理位置';
COMMENT ON COLUMN public.devices.sub_device_addr IS '子设备地址';
COMMENT ON COLUMN public.devices.current_version IS '当前固件版本';
COMMENT ON COLUMN public.devices.additional_info IS '其他信息 阈值、图片等';
COMMENT ON COLUMN public.devices.protocol_config IS '协议表单配置';
COMMENT ON COLUMN public.devices.device_config_id IS '设备配置id（外键）

';
COMMENT ON COLUMN public.devices.batch_number IS '批次编号
';
COMMENT ON COLUMN public.devices.activate_at IS '激活日期';
COMMENT ON COLUMN public.devices.is_online IS '是否在线 1-在线 0-离线';
COMMENT ON COLUMN public.devices.access_way IS '接入方式A-通过协议 B通过服务';
COMMENT ON COLUMN public.devices.description IS '描述';


-- public.event_datas definition

-- Drop table

-- DROP TABLE public.event_datas;

CREATE TABLE public.event_datas (
	id varchar(36) NOT NULL,
	device_id varchar(36) NOT NULL, -- 设备id（外键-关联删除）
	identify varchar(255) NOT NULL, -- 数据标识符
	ts timestamptz(6) NOT NULL, -- 上报时间
	"data" json NULL, -- 数据
	tenant_id varchar(36) NULL,
	CONSTRAINT event_datas_pkey PRIMARY KEY (id),
	CONSTRAINT event_datas_device_id_fkey FOREIGN KEY (device_id) REFERENCES public.devices(id) ON DELETE RESTRICT
);

-- Column comments

COMMENT ON COLUMN public.event_datas.device_id IS '设备id（外键-关联删除）';
COMMENT ON COLUMN public.event_datas.identify IS '数据标识符';
COMMENT ON COLUMN public.event_datas.ts IS '上报时间';
COMMENT ON COLUMN public.event_datas."data" IS '数据';


-- public.ota_upgrade_task_details definition

-- Drop table

-- DROP TABLE public.ota_upgrade_task_details;

CREATE TABLE public.ota_upgrade_task_details (
	id varchar(36) NOT NULL, -- Id
	ota_upgrade_task_id varchar(200) NOT NULL, -- 升级任务id（外键关联删除）
	device_id varchar(200) NOT NULL, -- 设备id（外键阻止删除）
	steps int2 NULL, -- 升级进度1-100
	status int2 NOT NULL, -- 状态1-待推送2-已推送3-升级中4-升级成功-5-升级失败-6已取消
	status_description varchar(500) NULL, -- 状态描述
	updated_at timestamptz(6) NULL,
	remark varchar(255) NULL,
	CONSTRAINT ota_upgrade_task_details_pkey PRIMARY KEY (id),
	CONSTRAINT fk_ota_upgrade_tasks FOREIGN KEY (ota_upgrade_task_id) REFERENCES public.ota_upgrade_tasks(id) ON DELETE CASCADE,
	CONSTRAINT ota_upgrade_task_details_device_id_fkey FOREIGN KEY (device_id) REFERENCES public.devices(id) ON DELETE RESTRICT
);

-- Column comments

COMMENT ON COLUMN public.ota_upgrade_task_details.id IS 'Id';
COMMENT ON COLUMN public.ota_upgrade_task_details.ota_upgrade_task_id IS '升级任务id（外键关联删除）';
COMMENT ON COLUMN public.ota_upgrade_task_details.device_id IS '设备id（外键阻止删除）';
COMMENT ON COLUMN public.ota_upgrade_task_details.steps IS '升级进度1-100';
COMMENT ON COLUMN public.ota_upgrade_task_details.status IS '状态1-待推送2-已推送3-升级中4-升级成功-5-升级失败-6已取消';
COMMENT ON COLUMN public.ota_upgrade_task_details.status_description IS '状态描述';


-- public.r_group_device definition

-- Drop table

-- DROP TABLE public.r_group_device;

CREATE TABLE public.r_group_device (
	group_id varchar(36) NOT NULL,
	device_id varchar(36) NOT NULL,
	tenant_id varchar(36) NOT NULL,
	CONSTRAINT r_group_device_group_id_device_id_key UNIQUE (group_id, device_id),
	CONSTRAINT fk_group_device FOREIGN KEY (group_id) REFERENCES public."groups"(id) ON DELETE CASCADE,
	CONSTRAINT fk_group_device_2 FOREIGN KEY (device_id) REFERENCES public.devices(id) ON DELETE CASCADE
);


-- public.telemetry_set_logs definition

-- Drop table

-- DROP TABLE public.telemetry_set_logs;

CREATE TABLE public.telemetry_set_logs (
	id varchar(36) NOT NULL,
	device_id varchar(36) NOT NULL, -- 设备id（外键-关联删除）
	operation_type varchar(255) NULL, -- 操作类型1-手动操作 2-自动触发
	"data" json NULL, -- 发送内容
	status varchar(2) NULL, -- 1-发送成功 2-失败
	error_message varchar(500) NULL, -- 错误信息
	created_at timestamptz(6) NOT NULL, -- 创建时间
	user_id varchar(36) NULL, -- 操作用户
	description varchar(255) NULL, -- 描述
	CONSTRAINT telemetry_set_logs_pkey PRIMARY KEY (id),
	CONSTRAINT telemetry_set_logs_device_id_fkey FOREIGN KEY (device_id) REFERENCES public.devices(id) ON DELETE RESTRICT
);

-- Column comments

COMMENT ON COLUMN public.telemetry_set_logs.device_id IS '设备id（外键-关联删除）';
COMMENT ON COLUMN public.telemetry_set_logs.operation_type IS '操作类型1-手动操作 2-自动触发';
COMMENT ON COLUMN public.telemetry_set_logs."data" IS '发送内容';
COMMENT ON COLUMN public.telemetry_set_logs.status IS '1-发送成功 2-失败';
COMMENT ON COLUMN public.telemetry_set_logs.error_message IS '错误信息';
COMMENT ON COLUMN public.telemetry_set_logs.created_at IS '创建时间';
COMMENT ON COLUMN public.telemetry_set_logs.user_id IS '操作用户';
COMMENT ON COLUMN public.telemetry_set_logs.description IS '描述';


-- public.attribute_datas definition

-- Drop table

-- DROP TABLE public.attribute_datas;

CREATE TABLE public.attribute_datas (
	id varchar(36) NOT NULL,
	device_id varchar(36) NOT NULL, -- 设备id（外键-关联删除）
	"key" varchar(255) NOT NULL, -- 数据标识符
	ts timestamptz(6) NOT NULL, -- 上报时间
	bool_v bool NULL,
	number_v float8 NULL,
	string_v text NULL,
	tenant_id varchar(36) NULL,
	CONSTRAINT attribute_datas_device_id_key_key UNIQUE (device_id, key),
	CONSTRAINT attribute_datas_device_id_fkey FOREIGN KEY (device_id) REFERENCES public.devices(id) ON DELETE RESTRICT
);

-- Column comments

COMMENT ON COLUMN public.attribute_datas.device_id IS '设备id（外键-关联删除）';
COMMENT ON COLUMN public.attribute_datas."key" IS '数据标识符';
COMMENT ON COLUMN public.attribute_datas.ts IS '上报时间';


-- public.attribute_set_logs definition

-- Drop table

-- DROP TABLE public.attribute_set_logs;

CREATE TABLE public.attribute_set_logs (
	id varchar(36) NOT NULL,
	device_id varchar(36) NOT NULL, -- 设备id（外键-关联删除）
	operation_type varchar(255) NULL, -- 操作类型1-手动操作 2-自动触发
	message_id varchar(36) NULL, -- 消息ID
	"data" text NULL, -- 发送内容
	rsp_data text NULL, -- 返回内容
	status varchar(2) NULL, -- 1-发送成功 2-失败
	error_message varchar(500) NULL, -- 错误信息
	created_at timestamptz(6) NOT NULL, -- 创建时间
	user_id varchar(36) NULL, -- 操作用户
	description varchar(255) NULL, -- 描述
	CONSTRAINT attribute_set_logs_pkey PRIMARY KEY (id),
	CONSTRAINT attribute_set_logs_device_id_fkey FOREIGN KEY (device_id) REFERENCES public.devices(id) ON DELETE RESTRICT
);

-- Column comments

COMMENT ON COLUMN public.attribute_set_logs.device_id IS '设备id（外键-关联删除）';
COMMENT ON COLUMN public.attribute_set_logs.operation_type IS '操作类型1-手动操作 2-自动触发';
COMMENT ON COLUMN public.attribute_set_logs.message_id IS '消息ID';
COMMENT ON COLUMN public.attribute_set_logs."data" IS '发送内容';
COMMENT ON COLUMN public.attribute_set_logs.rsp_data IS '返回内容';
COMMENT ON COLUMN public.attribute_set_logs.status IS '1-发送成功 2-失败';
COMMENT ON COLUMN public.attribute_set_logs.error_message IS '错误信息';
COMMENT ON COLUMN public.attribute_set_logs.created_at IS '创建时间';
COMMENT ON COLUMN public.attribute_set_logs.user_id IS '操作用户';
COMMENT ON COLUMN public.attribute_set_logs.description IS '描述';


-- public.command_set_logs definition

-- Drop table

-- DROP TABLE public.command_set_logs;

CREATE TABLE public.command_set_logs (
	id varchar(36) NOT NULL,
	device_id varchar(36) NOT NULL, -- 设备id（外键-关联删除）
	operation_type varchar(255) NULL, -- 操作类型1-手动操作 2-自动触发
	message_id varchar(36) NULL, -- 消息ID
	"data" text NULL, -- 发送内容
	rsp_data text NULL, -- 返回内容
	status varchar(2) NULL, -- 1-发送成功 2-失败
	error_message varchar(500) NULL, -- 错误信息
	created_at timestamptz(6) NOT NULL, -- 创建时间
	user_id varchar(36) NULL, -- 操作用户
	description varchar(255) NULL, -- 描述
	identify varchar(255) NULL, -- 数据标识符
	CONSTRAINT command_set_logs_pkey PRIMARY KEY (id),
	CONSTRAINT command_set_logs_device_id_fkey FOREIGN KEY (device_id) REFERENCES public.devices(id) ON DELETE RESTRICT
);
COMMENT ON TABLE public.command_set_logs IS '命令下发记录';

-- Column comments

COMMENT ON COLUMN public.command_set_logs.device_id IS '设备id（外键-关联删除）';
COMMENT ON COLUMN public.command_set_logs.operation_type IS '操作类型1-手动操作 2-自动触发';
COMMENT ON COLUMN public.command_set_logs.message_id IS '消息ID';
COMMENT ON COLUMN public.command_set_logs."data" IS '发送内容';
COMMENT ON COLUMN public.command_set_logs.rsp_data IS '返回内容';
COMMENT ON COLUMN public.command_set_logs.status IS '1-发送成功 2-失败';
COMMENT ON COLUMN public.command_set_logs.error_message IS '错误信息';
COMMENT ON COLUMN public.command_set_logs.created_at IS '创建时间';
COMMENT ON COLUMN public.command_set_logs.user_id IS '操作用户';
COMMENT ON COLUMN public.command_set_logs.description IS '描述';
COMMENT ON COLUMN public.command_set_logs.identify IS '数据标识符';

-- 初始化sql
INSERT INTO public.sys_dict (id, dict_code, dict_value, created_at, remark) VALUES('0013fb9e-e3be-95d4-9c96-f18d1f9ddfcd', 'GATEWAY_PROTOCOL', 'MQTT', '2024-01-18 15:39:38.469', NULL);
INSERT INTO public.sys_dict (id, dict_code, dict_value, created_at, remark) VALUES('7162fb9e-e3be-95d4-9c96-f18d1f9ddfcd', 'DRIECT_ATTACHED_PROTOCOL', 'MQTT', '2024-01-18 15:39:38.469', NULL);

INSERT INTO public.sys_dict_language (id, dict_id, language_code, "translation") VALUES('001c3960-3067-536d-5c97-7645351a687c', '7162fb9e-e3be-95d4-9c96-f18d1f9ddfcd', 'zh', 'MQTT协议');
INSERT INTO public.sys_dict_language (id, dict_id, language_code, "translation") VALUES('002c3960-3067-536d-5c97-7645351a687b', '0013fb9e-e3be-95d4-9c96-f18d1f9ddfcd', 'zh', 'MQTT协议(网关)');


INSERT INTO public.sys_function (id, "name", enable_flag, description, remark) VALUES('function_1', 'use_captcha', 'disable', '验证码登陆', NULL);
INSERT INTO public.sys_function (id, "name", enable_flag, description, remark) VALUES('function_2', 'enable_reg', 'disable', '租户注册', NULL);

INSERT INTO public.users (id, "name", phone_number, email, status, authority, "password", tenant_id, remark, additional_info, created_at, updated_at) VALUES('00000000-4fe9-b409-67c3-000000000000', 'admin', '1231231321', 'super@super.cn', 'N', 'SYS_ADMIN', '$2a$10$dPDIqoOEt.rSDwEWsSHCqe9/PJEsnWvRK76DwXVZUFM/7J0D3ikfq', '', 'dolor', '{}'::json, NULL, '2024-03-06 14:52:52.390');
INSERT INTO public.users (id, "name", phone_number, email, status, authority, "password", tenant_id, remark, additional_info, created_at, updated_at) VALUES('11111111-4fe9-b409-67c3-111111111111', 'Tenant', '17366666666', 'tenant@tenant.cn', 'N', 'TENANT_ADMIN', '$2a$10$zvPRDn0okgLt1t/OjQ.K5eZjGc3Mva7tmA8VlASsP8flfv0PwEz76', 'd616bcbb', '', '{}'::json, '2024-06-05 16:48:11.097', '2024-06-05 16:48:11.097');

INSERT INTO public.data_policy (id, data_type, retention_days, last_cleanup_time, last_cleanup_data_time, enabled, remark) VALUES('b', '2', 15, '2024-06-05 10:02:00.003', '2024-05-21 10:02:00.003', '1', '');
INSERT INTO public.data_policy (id, data_type, retention_days, last_cleanup_time, last_cleanup_data_time, enabled, remark) VALUES('a', '1', 15, '2024-06-05 10:02:00.003', '2024-05-21 10:02:00.101', '1', '');

INSERT INTO public.sys_ui_elements (id, parent_id, element_code, element_type, orders, param1, param2, param3, authority, description, created_at, remark, multilingual, route_path) VALUES('6e5e0963-46bf-bc27-d792-156e87a69f51', '6e5e0963-46bf-bc27-d792-156e87a69f51', 'alarm', 1, 115, '/alarm', 'simple-icons:antdesign', 'self', '["TENANT_ADMIN","SYS_ADMIN"]'::json, '告警', '2024-03-07 21:46:40.055', '', 'route.alarm', 'layout.base');
INSERT INTO public.sys_ui_elements (id, parent_id, element_code, element_type, orders, param1, param2, param3, authority, description, created_at, remark, multilingual, route_path) VALUES('36c4f5ce-3279-55f2-ede2-81b4a0bae24b', 'e1ebd134-53df-3105-35f4-489fc674d173', 'management_user', 3, 41, '/management/user', 'ic:round-manage-accounts', 'self', '["SYS_ADMIN"]'::json, '租户管理 ', '2024-02-18 17:50:48.999', '', 'route.management_user', 'view.management_user');
INSERT INTO public.sys_ui_elements (id, parent_id, element_code, element_type, orders, param1, param2, param3, authority, description, created_at, remark, multilingual, route_path) VALUES('367dbdb9-f28b-7a49-b8cd-23a915015093', 'a2c53126-029f-7138-4d7a-f45491f396da', 'apply_service', 3, 31, '/apply/service', 'uil:brightness-plus', 'self', '["SYS_ADMIN"]'::json, '协议插件管理', '2024-02-18 18:00:07.544', '', 'route.apply_service', 'view.apply_service');
INSERT INTO public.sys_ui_elements (id, parent_id, element_code, element_type, orders, param1, param2, param3, authority, description, created_at, remark, multilingual, route_path) VALUES('f9bd5f79-291e-26d2-1553-473c04b15ce4', 'e1ebd134-53df-3105-35f4-489fc674d173', 'management_setting', 3, 42, '/management/setting', 'uil:brightness-plus', 'self', '["SYS_ADMIN"]'::json, '系统设置', '2024-02-18 17:52:08.236', '', 'route.management_setting', 'view.management_setting');
INSERT INTO public.sys_ui_elements (id, parent_id, element_code, element_type, orders, param1, param2, param3, authority, description, created_at, remark, multilingual, route_path) VALUES('fec91838-d30d-7d66-6715-0912f1b171d8', 'e1ebd134-53df-3105-35f4-489fc674d173', 'management_notification', 3, 44, '/management/notification', 'mdi:alert', 'self', '["SYS_ADMIN"]'::json, '通知服务配置', '2024-03-15 19:50:07.495', '', 'route.management_notification', 'view.management_notification');
INSERT INTO public.sys_ui_elements (id, parent_id, element_code, element_type, orders, param1, param2, param3, authority, description, created_at, remark, multilingual, route_path) VALUES('75785418-a5af-d790-0783-e4ee4e42521e', '5373a6a2-1861-af35-eb4c-adfd5ca55ecd', 'device_grouping', 3, 1122, '/device/grouping', 'material-symbols:grid-on-outline-sharp', 'self', '["TENANT_ADMIN"]'::json, '设备分组', '2024-03-05 17:53:25.004', '', 'route.device_grouping', 'view.device_grouping');
INSERT INTO public.sys_ui_elements (id, parent_id, element_code, element_type, orders, param1, param2, param3, authority, description, created_at, remark, multilingual, route_path) VALUES('c4dff952-3bf4-8102-6882-e9d3f3cffbda', '5373a6a2-1861-af35-eb4c-adfd5ca55ecd', 'device_manage', 3, 1121, '/device/manage', 'icon-park-outline:analysis', 'self', '["TENANT_ADMIN"]'::json, '设备管理', '2024-03-05 17:55:08.170', '', 'route.device_manage', 'view.device_manage');
INSERT INTO public.sys_ui_elements (id, parent_id, element_code, element_type, orders, param1, param2, param3, authority, description, created_at, remark, multilingual, route_path) VALUES('51381989-1160-93cd-182e-d44a1c4ab89b', '676e8f33-875a-0473-e9ca-c82fd09fef57', 'automation_scene-manage', 3, 1142, '/automation/scene-manage', 'uil:brightness-plus', 'self', '["TENANT_ADMIN","SYS_ADMIN"]'::json, '场景管理', '2024-03-07 21:44:11.106', '', 'route.automation_scene-manage', 'view.automation_scene-manage');
INSERT INTO public.sys_ui_elements (id, parent_id, element_code, element_type, orders, param1, param2, param3, authority, description, created_at, remark, multilingual, route_path) VALUES('774a716d-9861-bac9-857f-acaa25e7659f', '5373a6a2-1861-af35-eb4c-adfd5ca55ecd', 'device_config', 3, 1126, '/device/config', 'clarity:plugin-line', 'self', '["TENANT_ADMIN"]'::json, '配置模板', '2024-03-05 22:06:53.842', '设备的协议和其他参数等所有配置', 'route.device_config', 'view.device_config');
INSERT INTO public.sys_ui_elements (id, parent_id, element_code, element_type, orders, param1, param2, param3, authority, description, created_at, remark, multilingual, route_path) VALUES('8f4e9058-e30d-2fb5-ac6d-784613234883', '6e5e0963-46bf-bc27-d792-156e87a69f51', 'alarm-information', 3, 1151, '/alarm/alarm-information', 'ph:alarm', 'basic', '["TENANT_ADMIN"]'::json, '告警信息', '2024-03-07 21:47:22.817', '', 'default', '');
INSERT INTO public.sys_ui_elements (id, parent_id, element_code, element_type, orders, param1, param2, param3, authority, description, created_at, remark, multilingual, route_path) VALUES('b6d57a4a-d37a-9d9d-6e4e-be33b955ff04', '6e5e0963-46bf-bc27-d792-156e87a69f51', 'alarm_notification-group', 3, 1152, '/alarm/notification-group', 'simple-icons:apacheecharts', 'basic', '["TENANT_ADMIN"]'::json, '通知组', '2024-03-07 21:48:15.416', '', 'route.alarm_notification-group', 'view.alarm_notification-group');
INSERT INTO public.sys_ui_elements (id, parent_id, element_code, element_type, orders, param1, param2, param3, authority, description, created_at, remark, multilingual, route_path) VALUES('49857e46-2176-610e-98fc-892b4fde50f9', '5373a6a2-1861-af35-eb4c-adfd5ca55ecd', 'device_details', 3, 1124, '/device/details', 'mdi:monitor-dashboard', '1', '["TENANT_ADMIN"]'::json, '设备详情', '2024-03-05 17:52:21.434', '', 'route.device_details', 'view.device_details');
INSERT INTO public.sys_ui_elements (id, parent_id, element_code, element_type, orders, param1, param2, param3, authority, description, created_at, remark, multilingual, route_path) VALUES('8de46003-170c-a24d-6baf-84d1c7298aa3', '5373a6a2-1861-af35-eb4c-adfd5ca55ecd', 'device_grouping-details', 3, 1123, '/device/grouping-details', '', '1', '["TENANT_ADMIN"]'::json, '分组详情', '2024-03-05 17:54:23.158', '', 'route.device_grouping-details', 'view.device_grouping-details');
INSERT INTO public.sys_ui_elements (id, parent_id, element_code, element_type, orders, param1, param2, param3, authority, description, created_at, remark, multilingual, route_path) VALUES('faf7e607-00ae-3483-40a1-b74f9245b100', 'e1ebd134-53df-3105-35f4-489fc674d173', 'management_auth', 3, 43, '/management/auth', 'ic:baseline-security', 'self', '["SYS_ADMIN"]'::json, '菜单管理', '2024-02-18 17:49:31.209', '', 'route.management_auth', 'view.management_auth');
INSERT INTO public.sys_ui_elements (id, parent_id, element_code, element_type, orders, param1, param2, param3, authority, description, created_at, remark, multilingual, route_path) VALUES('a2c53126-029f-7138-4d7a-f45491f396da', '0', 'apply', 1, 3, '/apply', 'mdi:apps-box', 'self', '["SYS_ADMIN"]'::json, '应用管理', '2024-02-18 17:59:31.642', '', 'route.apply', 'layout.base');
INSERT INTO public.sys_ui_elements (id, parent_id, element_code, element_type, orders, param1, param2, param3, authority, description, created_at, remark, multilingual, route_path) VALUES('5373a6a2-1861-af35-eb4c-adfd5ca55ecd', '0', 'device', 1, 112, '/device', 'icon-park-outline:workbench', 'self', '["TENANT_ADMIN"]'::json, '设备接入', '2024-03-05 17:51:19.298', '', 'route.device', 'layout.base');
INSERT INTO public.sys_ui_elements (id, parent_id, element_code, element_type, orders, param1, param2, param3, authority, description, created_at, remark, multilingual, route_path) VALUES('95e2a961-382b-f4a6-87b3-1898123c95bc', '0', 'visualization', 1, 113, '/visualization', 'icon-park-outline:data-server', 'self', '["TENANT_ADMIN","SYS_ADMIN"]'::json, '可视化', '2024-03-07 21:37:16.042', '', 'route.visualization', 'layout.base');
INSERT INTO public.sys_ui_elements (id, parent_id, element_code, element_type, orders, param1, param2, param3, authority, description, created_at, remark, multilingual, route_path) VALUES('676e8f33-875a-0473-e9ca-c82fd09fef57', '0', 'automation', 1, 114, '/automation', 'material-symbols:device-hub', 'self', '["TENANT_ADMIN"]'::json, '自动化', '2024-03-07 21:41:17.921', '', 'route.automation', 'layout.base');
INSERT INTO public.sys_ui_elements (id, parent_id, element_code, element_type, orders, param1, param2, param3, authority, description, created_at, remark, multilingual, route_path) VALUES('e1ebd134-53df-3105-35f4-489fc674d173', '0', 'management', 1, 120, '/management', 'carbon:cloud-service-management', 'self', '["SYS_ADMIN","TENANT_ADMIN"]'::json, '系统管理', '2024-02-18 17:48:45.265', '', 'route.management', 'layout.base');
INSERT INTO public.sys_ui_elements (id, parent_id, element_code, element_type, orders, param1, param2, param3, authority, description, created_at, remark, multilingual, route_path) VALUES('e186a671-8e24-143a-5a2c-27a1f5f38bf3', '5373a6a2-1861-af35-eb4c-adfd5ca55ecd', 'device_config-edit', 3, 1128, '/device/config-edit', '', '1', '["SYS_ADMIN","TENANT_ADMIN"]'::json, '设备配置编辑', '2024-03-11 21:49:34.952', '', 'route.device_config-edit', 'view.device_config-edit');
INSERT INTO public.sys_ui_elements (id, parent_id, element_code, element_type, orders, param1, param2, param3, authority, description, created_at, remark, multilingual, route_path) VALUES('502a0d6c-750e-92f6-a1a7-ffdd362dbbac', '95e2a961-382b-f4a6-87b3-1898123c95bc', 'visualization_panel-preview', 3, 1133, '/visualization/panel-preview', '', '1', '["TENANT_ADMIN","SYS_ADMIN"]'::json, '看板预览', '2024-03-12 10:16:29.336', '', 'route.visualization_panel-preview', 'view.visualization_panel-preview');
INSERT INTO public.sys_ui_elements (id, parent_id, element_code, element_type, orders, param1, param2, param3, authority, description, created_at, remark, multilingual, route_path) VALUES('96aa2fac-90b2-aca1-1ce0-51b5060f4081', '676e8f33-875a-0473-e9ca-c82fd09fef57', 'automation_linkage-edit', 3, 1143, '/automation/linkage-edit', '', '1', '["TENANT_ADMIN","SYS_ADMIN"]'::json, '场景联动编辑', '2024-03-15 01:36:03.938', '', 'route.automation_linkage-edit', 'view.automation_linkage-edit');
INSERT INTO public.sys_ui_elements (id, parent_id, element_code, element_type, orders, param1, param2, param3, authority, description, created_at, remark, multilingual, route_path) VALUES('5f3aba62-da10-1e62-38c5-8eda5ef1f899', '0', 'system-management-user', 3, 117, '/system-management-user', 'mdi:monitor-dashboard', '1', '["SYS_ADMIN","TENANT_ADMIN"]'::json, '系统管理(租户)', '2024-03-07 22:03:51.424', '', 'route.system-management-user', 'layout.base');
INSERT INTO public.sys_ui_elements (id, parent_id, element_code, element_type, orders, param1, param2, param3, authority, description, created_at, remark, multilingual, route_path) VALUES('7419e37e-c167-f12b-7ace-76e479144181', '5373a6a2-1861-af35-eb4c-adfd5ca55ecd', 'device_template', 3, 1127, '/device/template', 'simple-icons:apacheecharts', 'self', '["TENANT_ADMIN"]'::json, '功能模板', '2024-03-05 18:01:29.826', '定义物模型和显示图表', 'route.device_template', 'view.device_template');
INSERT INTO public.sys_ui_elements (id, parent_id, element_code, element_type, orders, param1, param2, param3, authority, description, created_at, remark, multilingual, route_path) VALUES('e619f321-9823-b563-b24d-ecc16d7b23cc', '6e5e0963-46bf-bc27-d792-156e87a69f51', 'alarm_notification-record', 3, 1153, '/alarm/notification-record', 'mdi:monitor-dashboard', 'basic', '["TENANT_ADMIN"]'::json, '通知记录', '2024-03-07 21:48:56.415', '', 'route.alarm_notification-record', 'view.alarm_notification-record');
INSERT INTO public.sys_ui_elements (id, parent_id, element_code, element_type, orders, param1, param2, param3, authority, description, created_at, remark, multilingual, route_path) VALUES('01dab674-9556-cdd7-b800-78bcb366adb4', '676e8f33-875a-0473-e9ca-c82fd09fef57', 'automation_scene-linkage', 3, 1141, '/automation/scene-linkage', 'mdi:airplane-edit', 'self', '["TENANT_ADMIN","SYS_ADMIN"]'::json, '场景联动', '2024-03-07 21:43:33.920', '', 'route.automation_scene-linkage', 'view.automation_scene-linkage');
INSERT INTO public.sys_ui_elements (id, parent_id, element_code, element_type, orders, param1, param2, param3, authority, description, created_at, remark, multilingual, route_path) VALUES('a2654c98-3749-c88b-0472-b414049ca532', '95e2a961-382b-f4a6-87b3-1898123c95bc', 'route.visualization_kanban', 3, 1131, '/visualization/kanban', 'tabler:device-tv', 'self', '["TENANT_ADMIN","SYS_ADMIN"]'::json, '看板', '2024-03-07 21:39:58.608', '', 'route.visualization_kanban', 'view.visualization_panel');
INSERT INTO public.sys_ui_elements (id, parent_id, element_code, element_type, orders, param1, param2, param3, authority, description, created_at, remark, multilingual, route_path) VALUES('ed4a5cfa-03e7-ccc0-6cc8-bcadccd25541', '95e2a961-382b-f4a6-87b3-1898123c95bc', 'visualization_kanban-details', 3, 1132, '/visualization/kanban-details', 'ic:baseline-credit-card', '1', '["TENANT_ADMIN","SYS_ADMIN"]'::json, '看板详情', '2024-03-12 10:14:50.152', '', '看板详情', 'view.visualization_panel-details');
INSERT INTO public.sys_ui_elements (id, parent_id, element_code, element_type, orders, param1, param2, param3, authority, description, created_at, remark, multilingual, route_path) VALUES('c078182f-bf4b-b560-da97-02926fa98f78', '650bc444-7672-1123-1e41-7e37365b0186', 'alarm_notification-record', 3, 1, '/alarm/notification-record', 'icon-park-outline:editor', 'self', '["TENANT_ADMIN"]'::json, '通知记录', '2024-03-20 10:04:34.927', '', 'route.alarm_notification-record', 'view.alarm_notification-record');
INSERT INTO public.sys_ui_elements (id, parent_id, element_code, element_type, orders, param1, param2, param3, authority, description, created_at, remark, multilingual, route_path) VALUES('485c2a20-ebc5-2216-4871-26453470d290', '650bc444-7672-1123-1e41-7e37365b0186', 'alarm_warning-message', 3, 999, '/alarm/warning-message', 'mdi:airballoon', 'self', '["TENANT_ADMIN"]'::json, '警告信息', '2024-03-17 15:27:40.378', '', 'route.alarm_warning-message', 'view.alarm_warning-message');
INSERT INTO public.sys_ui_elements (id, parent_id, element_code, element_type, orders, param1, param2, param3, authority, description, created_at, remark, multilingual, route_path) VALUES('2f3ffd60-efec-aafb-a866-f1cb79f88390', 'e1ebd134-53df-3105-35f4-489fc674d173', 'system-management-user_system-log', 3, 1171, '/system-management-user/system-log', 'mdi:monitor-dashboard', 'basic', '["TENANT_ADMIN"]'::json, '系统日志', '2024-03-07 22:23:08.576', '', 'route.system-management-user_system-log', 'view.system-management-user_system-log');
INSERT INTO public.sys_ui_elements (id, parent_id, element_code, element_type, orders, param1, param2, param3, authority, description, created_at, remark, multilingual, route_path) VALUES('82c46beb-9ec4-8a3d-c6e4-04ba426e525a', '650bc444-7672-1123-1e41-7e37365b0186', 'alarm_notification-group', 3, 1, '/alarm/notification-group', 'ic:round-supervisor-account', 'basic', '["TENANT_ADMIN"]'::json, '通知组', '2024-03-20 10:03:19.955', '', 'route.alarm_notification-group', 'view.alarm_notification-group');
INSERT INTO public.sys_ui_elements (id, parent_id, element_code, element_type, orders, param1, param2, param3, authority, description, created_at, remark, multilingual, route_path) VALUES('59612e2f-e297-acb7-fcf4-143bf6e66109', '5373a6a2-1861-af35-eb4c-adfd5ca55ecd', 'device_details-child', 3, 1124, '/device/details-child', '', '1', '["TENANT_ADMIN"]'::json, '子设备详情', '2024-05-10 20:33:34.869', '', 'route.device_details-child', 'view.device_details-child');
INSERT INTO public.sys_ui_elements (id, parent_id, element_code, element_type, orders, param1, param2, param3, authority, description, created_at, remark, multilingual, route_path) VALUES('3aaca04b-2a2e-dfca-9fb4-0b2819362783', '2cc0c5ba-f086-91e5-0b8c-ad0546b1f2a9', 'test_kan-ban-test', 3, 1, '/test/kan-ban-test', '', 'basic', '["SYS_ADMIN","TENANT_ADMIN"]'::json, '看板测试', '2024-05-21 01:17:16.911', '', 'route.test_kan-ban-test', 'view.test_kan-ban-test');
INSERT INTO public.sys_ui_elements (id, parent_id, element_code, element_type, orders, param1, param2, param3, authority, description, created_at, remark, multilingual, route_path) VALUES('650bc444-7672-1123-1e41-7e37365b0186', '0', 'alarm', 1, 115, '/alarm', 'mdi:alert', 'self', '["TENANT_ADMIN"]'::json, '告警', '2024-03-17 09:01:52.183', '', 'route.alarm', 'layout.base');
INSERT INTO public.sys_ui_elements (id, parent_id, element_code, element_type, orders, param1, param2, param3, authority, description, created_at, remark, multilingual, route_path) VALUES('a190f7a5-1501-3814-9dd1-f3e1fbe7265e', '0', 'home', 3, 0, '/home', 'mdi:alpha-f-box-outline', 'self', '["SYS_ADMIN","TENANT_ADMIN"]'::json, '首页', '2024-02-26 16:07:20.202', 'home', 'route.home', 'layout.base$view.home');
INSERT INTO public.sys_ui_elements (id, parent_id, element_code, element_type, orders, param1, param2, param3, authority, description, created_at, remark, multilingual, route_path) VALUES('86cb08fa-8b08-3d99-4b3a-d6132ee93a0f', '5373a6a2-1861-af35-eb4c-adfd5ca55ecd', 'device_config-detail', 3, 1127, '/device/config-detail', 'icon-park-outline:data-server', '1', '["SYS_ADMIN","TENANT_ADMIN"]'::json, '设备配置详情', '2024-03-10 11:13:25.253', '', 'route.device_config-detail', 'view.device_config-detail');
INSERT INTO public.sys_ui_elements (id, parent_id, element_code, element_type, orders, param1, param2, param3, authority, description, created_at, remark, multilingual, route_path) VALUES('64f684f1-390c-b5f2-9994-36895025df8a', '676e8f33-875a-0473-e9ca-c82fd09fef57', 'automation_space-management', 3, 10, 'automation/space-management', 'ic:baseline-security', '1', '["TENANT_ADMIN","SYS_ADMIN"]'::json, '场景管理', '2024-03-22 13:25:38.820', '', 'default', 'view.automation space-management');
INSERT INTO public.sys_ui_elements (id, parent_id, element_code, element_type, orders, param1, param2, param3, authority, description, created_at, remark, multilingual, route_path) VALUES('80cf264f-f905-e6e9-3095-35b03098edb2', 'e1ebd134-53df-3105-35f4-489fc674d173', 'system-management-user_equipment-map', 3, 9, '/system-management-user/equipment-map', 'mdi:chart-areaspline', '1', '["TENANT_ADMIN"]'::json, '设备地图', '2024-03-21 10:53:48.035', '', 'route.system-management-user_equipment-map', 'view.system-management-user_equipment-map');
INSERT INTO public.sys_ui_elements (id, parent_id, element_code, element_type, orders, param1, param2, param3, authority, description, created_at, remark, multilingual, route_path) VALUES('76bfc16e-ed22-bcc0-c688-d462666e8a8d', '0', 'personal-center', 3, 999, '/personal-center', 'carbon:user-role', '1', '["TENANT_ADMIN","SYS_ADMIN"]'::json, '个人中心', '2024-03-17 09:27:01.048', '', 'route.personal_center', 'layout.base$view.personal-center');
INSERT INTO public.sys_ui_elements (id, parent_id, element_code, element_type, orders, param1, param2, param3, authority, description, created_at, remark, multilingual, route_path) VALUES('975c9550-5db9-7b4c-5dea-7a4c326a37ff', '676e8f33-875a-0473-e9ca-c82fd09fef57', 'automation_scene-edit', 3, 1, '/automation/scene-edit', 'mdi:apps-box', '1', '["TENANT_ADMIN"]'::json, '新增场景', '2024-04-04 10:50:43.219', '', 'route.automation_scene-edit', 'view.automation_scene-edit');
INSERT INTO public.sys_ui_elements (id, parent_id, element_code, element_type, orders, param1, param2, param3, authority, description, created_at, remark, multilingual, route_path) VALUES('2cc0c5ba-f086-91e5-0b8c-ad0546b1f2a9', '0', 'test', 1, 1, '/test', 'ic:round-construction', '1', '["SYS_ADMIN","TENANT_ADMIN"]'::json, '测试', '2024-04-18 05:12:18.972', '测试用，后面影藏', 'route.test', 'layout.base');
INSERT INTO public.sys_ui_elements (id, parent_id, element_code, element_type, orders, param1, param2, param3, authority, description, created_at, remark, multilingual, route_path) VALUES('680cae76-6c50-90e6-c2f9-58d01389aa08', '9a11b3e4-9982-a0f0-996c-a9be6e738947', 'data-service_rule-engine', 3, 21, '/data-service/rule-engine', 'mdi:menu', '1', '["SYS_ADMIN"]'::json, '规则引擎', '2024-03-07 17:06:02.804', '', 'route.data-service_rule-engine', 'view.data-service_rule-engine');
INSERT INTO public.sys_ui_elements (id, parent_id, element_code, element_type, orders, param1, param2, param3, authority, description, created_at, remark, multilingual, route_path) VALUES('9a11b3e4-9982-a0f0-996c-a9be6e738947', '0', 'data-service', 1, 2, '/data-service', 'mdi:monitor-dashboard', '1', '["SYS_ADMIN"]'::json, '数据服务', '2024-03-07 17:05:04.101', '', 'route.data-service', 'layout.base');

INSERT INTO public.logo (id, system_name, logo_cache, logo_background, logo_loading, home_background, remark) VALUES('a', 'ThingsPanel', '', '', '', '', NULL);