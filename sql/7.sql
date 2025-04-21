-- ✅2025/4/17
ALTER TABLE public.scene_action_info ALTER COLUMN action_param_type TYPE varchar(20) USING action_param_type::varchar(20);

-- ✅2025/4/21
ALTER TABLE public.devices ALTER COLUMN device_number TYPE varchar(100) USING device_number::varchar(100);

