-- Restore the public sourceType for events accepted by the legacy alarm
-- ingress. Business IDs, intent digests, deliveries and dispatch state stay
-- unchanged; this migration never requeues a send.
UPDATE notification_requests
SET source_type = 'alarm'
WHERE source_type = 'source' AND source_deployment_id IS NOT NULL;
