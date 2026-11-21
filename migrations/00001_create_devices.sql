-- +goose Up
CREATE TABLE devices (
    id           uuid        PRIMARY KEY DEFAULT gen_random_uuid(),
    org_id       text        NOT NULL,
    name         text        NOT NULL,
    platform     text        NOT NULL,
    status       text        NOT NULL DEFAULT 'offline' CHECK (status IN ('online', 'offline')),
    last_seen_at timestamptz,
    created_at   timestamptz NOT NULL DEFAULT now(),

    CONSTRAINT devices_org_id_name_key UNIQUE (org_id, name)
);

CREATE INDEX devices_online_last_seen_idx ON devices (last_seen_at) WHERE status = 'online';

-- +goose Down
DROP TABLE devices;
