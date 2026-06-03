ALTER TABLE public.device_trigger_condition
  ALTER COLUMN trigger_value TYPE varchar(1024) USING trigger_value::varchar(1024);
