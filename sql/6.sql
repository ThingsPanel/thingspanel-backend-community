-- ✅2025/3/20 v1.1.6

-- 为name字段创建lower索引
CREATE INDEX idx_lower_name ON public.devices (LOWER(name));

-- 为device_number字段创建lower索引
CREATE INDEX idx_lower_device_number ON public.devices (LOWER(device_number));

-- ✅2025/4/3
CREATE TABLE "public"."message_push_rule_log" (
                                                  "id" varchar(60) COLLATE "pg_catalog"."default" NOT NULL,
                                                  "user_id" varchar(60) COLLATE "pg_catalog"."default" NOT NULL,
                                                  "push_id" varchar(60) COLLATE "pg_catalog"."default" NOT NULL,
                                                  "type" int2 NOT NULL,
                                                  "create_time" timestamp(6) NOT NULL,
                                                  CONSTRAINT "message_pkey" PRIMARY KEY ("id")
)
;

ALTER TABLE "public"."message_push_rule_log"
    OWNER TO "postgres";

COMMENT ON COLUMN "public"."message_push_rule_log"."type" IS '1 主动失效 2被动失效 3定时任务 4自动清理';

COMMENT ON COLUMN "public"."message_push_rule_log"."create_time" IS '生效时间';

COMMENT ON TABLE "public"."message_push_rule_log" IS '失效规则记录';

CREATE TABLE "public"."message_push_manage" (
                                                "id" varchar(60) COLLATE "pg_catalog"."default" NOT NULL,
                                                "user_id" varchar(60) COLLATE "pg_catalog"."default" NOT NULL,
                                                "push_id" varchar(60) COLLATE "pg_catalog"."default" NOT NULL,
                                                "device_type" varchar(60) COLLATE "pg_catalog"."default" NOT NULL,
                                                "status" int2 NOT NULL DEFAULT 1,
                                                "create_time" timestamp(6) NOT NULL,
                                                "update_time" timestamp(6),
                                                "delete_time" timestamp(6),
                                                "last_push_time" timestamp(6),
                                                "err_count" int4 DEFAULT 0,
                                                "inactive_time" timestamp(6),
                                                CONSTRAINT "message_push_manage_pkey" PRIMARY KEY ("id")
)
;

ALTER TABLE "public"."message_push_manage"
    OWNER TO "postgres";

CREATE UNIQUE INDEX "index_user_push" ON "public"."message_push_manage" USING btree (
    "user_id" COLLATE "pg_catalog"."default" "pg_catalog"."text_ops" ASC NULLS LAST,
    "push_id" COLLATE "pg_catalog"."default" "pg_catalog"."text_ops" ASC NULLS LAST
    );

COMMENT ON COLUMN "public"."message_push_manage"."user_id" IS '用户id';

COMMENT ON COLUMN "public"."message_push_manage"."push_id" IS '推送id';

COMMENT ON COLUMN "public"."message_push_manage"."device_type" IS '设备类型';

COMMENT ON COLUMN "public"."message_push_manage"."status" IS '类型 1正常 2注销';

COMMENT ON COLUMN "public"."message_push_manage"."create_time" IS '创建类型';

COMMENT ON COLUMN "public"."message_push_manage"."update_time" IS '更新时间';

COMMENT ON COLUMN "public"."message_push_manage"."delete_time" IS '删除时间';

COMMENT ON COLUMN "public"."message_push_manage"."last_push_time" IS '最后一次推送时间';

COMMENT ON COLUMN "public"."message_push_manage"."err_count" IS '联系推送错误次数';

COMMENT ON COLUMN "public"."message_push_manage"."inactive_time" IS '标记不活跃时间';

COMMENT ON TABLE "public"."message_push_manage" IS '消息推送通知';

CREATE TABLE "public"."message_push_log" (
                                             "id" varchar(60) COLLATE "pg_catalog"."default" NOT NULL,
                                             "user_id" varchar(60) COLLATE "pg_catalog"."default" NOT NULL,
                                             "message_type" int8 NOT NULL,
                                             "content" json NOT NULL,
                                             "status" int2 NOT NULL,
                                             "err_message" varchar(255) COLLATE "pg_catalog"."default" NOT NULL,
                                             "create_time" timestamp(6) NOT NULL,
                                             CONSTRAINT "message_push_log_pkey" PRIMARY KEY ("id")
)
;

ALTER TABLE "public"."message_push_log"
    OWNER TO "postgres";

COMMENT ON COLUMN "public"."message_push_log"."user_id" IS '用户id';

COMMENT ON COLUMN "public"."message_push_log"."message_type" IS '消息类型 1告警消息';

COMMENT ON COLUMN "public"."message_push_log"."content" IS '消息体内容';

COMMENT ON COLUMN "public"."message_push_log"."status" IS '1推送成功 2推送失败';

COMMENT ON COLUMN "public"."message_push_log"."err_message" IS '错误信息';

COMMENT ON COLUMN "public"."message_push_log"."create_time" IS '发送时间';

COMMENT ON TABLE "public"."message_push_log" IS '消息推送日志';

CREATE TABLE "public"."message_push_config" (
                                                "id" varchar(60) COLLATE "pg_catalog"."default" NOT NULL,
                                                "url" varchar(255) COLLATE "pg_catalog"."default" NOT NULL,
                                                "config_type" int2 NOT NULL DEFAULT 1,
                                                "create_time" timestamp(6) NOT NULL,
                                                "update_time" timestamp(6),
                                                CONSTRAINT "message_push_config_pkey" PRIMARY KEY ("id")
)
;

ALTER TABLE "public"."message_push_config"
    OWNER TO "postgres";

COMMENT ON COLUMN "public"."message_push_config"."url" IS '推送地址';

COMMENT ON COLUMN "public"."message_push_config"."config_type" IS '配置类型 1 推送地址';

COMMENT ON COLUMN "public"."message_push_config"."create_time" IS '创建时间';

COMMENT ON COLUMN "public"."message_push_config"."update_time" IS '更新时间';

COMMENT ON TABLE "public"."message_push_config" IS '消息推送配置';


-- ✅2025/4/10
-- 创建视图，获取每个设备最新的告警记录
CREATE OR REPLACE VIEW public.latest_device_alarms AS
WITH unnested_devices AS (
  SELECT 
    ah.id,
    ah.alarm_config_id,
    ah.group_id,
    ah.scene_automation_id,
    ah.name,
    ah.description,
    ah.content,
    ah.alarm_status,
    ah.tenant_id,
    ah.remark,
    ah.create_at,
    jsonb_array_elements_text(ah.alarm_device_list) AS device_id
  FROM 
    public.alarm_history ah
),
ranked_alarms AS (
  SELECT 
    *,
    ROW_NUMBER() OVER (PARTITION BY device_id ORDER BY create_at DESC) as rn
  FROM 
    unnested_devices
)
SELECT 
  id,
  alarm_config_id,
  group_id,
  scene_automation_id,
  name,
  description,
  content,
  alarm_status,
  tenant_id,
  remark,
  create_at,
  device_id
FROM 
  ranked_alarms
WHERE 
  rn = 1;