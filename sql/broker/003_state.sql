-- Community connection observations, separate from legacy heartbeat semantics.
CREATE TABLE IF NOT EXISTS broker_connection_observations (
 tenant_id varchar(256) NOT NULL, device_id varchar(256) NOT NULL,
 broker_cell varchar(256) NOT NULL, broker_instance varchar(256) NOT NULL,
 node_boot_id varchar(256) NOT NULL, connection_id varchar(256) NOT NULL,
 route_epoch bigint NOT NULL CHECK(route_epoch>0), event_seq bigint NOT NULL CHECK(event_seq>0),
 online boolean NOT NULL, observed_at timestamptz NOT NULL, received_at timestamptz NOT NULL DEFAULT now(),
 PRIMARY KEY(tenant_id,device_id,broker_cell,broker_instance,node_boot_id,connection_id)
);
CREATE INDEX IF NOT EXISTS broker_observations_device_epoch ON broker_connection_observations(tenant_id,device_id,route_epoch);
