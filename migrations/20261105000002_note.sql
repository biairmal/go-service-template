-- +goose Up
-- The example business table: tenant- and owner-scoped, versioned for If-Match.
CREATE TABLE note (
    id         uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id  uuid NOT NULL,
    owner_id   uuid NOT NULL,
    title      text NOT NULL,
    body       text NOT NULL DEFAULT '',
    status     text NOT NULL DEFAULT 'draft' CHECK (status IN ('draft', 'archived')),
    pinned     boolean NOT NULL DEFAULT false,
    version    int NOT NULL DEFAULT 1,
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now(),
    created_by uuid NOT NULL
);
CREATE INDEX note_tenant_owner_updated ON note (tenant_id, owner_id, updated_at DESC);
CREATE INDEX note_tenant_status ON note (tenant_id, status);

-- +goose Down
DROP TABLE note;
