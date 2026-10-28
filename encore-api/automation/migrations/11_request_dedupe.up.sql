-- Inbound request dedupe (A27).
--
-- The outbound side has always had a stable key per intent (auto_outbox). The
-- inbound side did not: a client that timed out and retried POST /v1/rules/:id/run
-- produced a second run, and therefore a second device command. "The AI retried"
-- must not mean "the door opened twice".
--
-- The key travels in the request body as request_id, not in a header, because an
-- Encore typed handler cannot see headers. It is scoped per tenant so one
-- customer's key can never replay another customer's response.
CREATE TABLE IF NOT EXISTS auto_requests (
    tenant_id   text NOT NULL,
    request_key text NOT NULL,
    endpoint    text NOT NULL,
    response    jsonb NOT NULL,
    created_at  timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (tenant_id, request_key)
);

-- Old rows are only useful while a client might still retry; anything older
-- than a day is dead weight. This index lets the sweeper find them.
CREATE INDEX IF NOT EXISTS auto_requests_age ON auto_requests (created_at);
