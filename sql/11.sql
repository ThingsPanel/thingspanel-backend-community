-- ✅2025/9.9 增加可视化类型字段
ALTER TABLE public.boards ADD vis_type varchar(50) NULL;
COMMENT ON COLUMN public.boards.vis_type IS '可视化类型';
