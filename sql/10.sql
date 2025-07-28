-- ✅2025/7/28 自动化动作表字段action_param_type扩展长度

ALTER TABLE public.action_info ALTER COLUMN action_param_type TYPE varchar(50) USING action_param_type::varchar(50);
