ALTER TABLE public.device_trigger_condition
  ALTER COLUMN trigger_value TYPE varchar(1024) USING trigger_value::varchar(1024);

ALTER TABLE public.devices
  ALTER COLUMN voucher TYPE varchar(2048) USING voucher::varchar(2048);
