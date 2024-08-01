INSERT INTO public.sys_function (id, "name", enable_flag, description, remark) VALUES('function_3', 'frontend_res', 'disable', '前端RSA加密', NULL);

ALTER TABLE "public"."casbin_rule"
ALTER COLUMN "v0" TYPE varchar(200) COLLATE "pg_catalog"."default",
  ALTER COLUMN "v1" TYPE varchar(200) COLLATE "pg_catalog"."default",
  ALTER COLUMN "v2" TYPE varchar(200) COLLATE "pg_catalog"."default",
  ALTER COLUMN "v3" TYPE varchar(200) COLLATE "pg_catalog"."default",
  ALTER COLUMN "v4" TYPE varchar(200) COLLATE "pg_catalog"."default",
  ALTER COLUMN "v5" TYPE varchar(200) COLLATE "pg_catalog"."default";