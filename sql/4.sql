ALTER TABLE public.users ADD password_last_updated timestamptz(6) NULL;
INSERT INTO public.sys_function (id, "name", enable_flag, description, remark) VALUES('function_4', 'shared_account', 'enable', '共享账号', NULL);

ALTER TABLE public.users ADD last_visit_time timestamptz NULL;
COMMENT ON COLUMN public.users.last_visit_time IS '上次访问时间';
