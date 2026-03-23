CREATE TABLE IF NOT EXISTS users (
    id BIGSERIAL PRIMARY KEY,
    login TEXT NOT NULL UNIQUE,
    password_hash TEXT NOT NULL,
    role TEXT NOT NULL CHECK (role IN ('ADMIN', 'USER')),
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE TABLE IF NOT EXISTS project_types (
    id BIGSERIAL PRIMARY KEY,
    name TEXT NOT NULL UNIQUE,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE TABLE IF NOT EXISTS external_applications (
    id BIGSERIAL PRIMARY KEY,
    full_name TEXT NOT NULL,
    email TEXT NOT NULL,
    phone TEXT,
    organisation_name TEXT NOT NULL,
    organisation_url TEXT,
    project_name TEXT NOT NULL,
    project_type_id BIGINT NOT NULL REFERENCES project_types(id),
    expected_results TEXT NOT NULL,
    is_payed BOOLEAN NOT NULL,
    additional_information TEXT,
    rejection_reason TEXT,
    status TEXT NOT NULL CHECK (status IN ('PENDING', 'ACCEPTED', 'REJECTED')),
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS idx_external_applications_status ON external_applications(status);
CREATE INDEX IF NOT EXISTS idx_external_applications_project_type_id ON external_applications(project_type_id);
CREATE INDEX IF NOT EXISTS idx_external_applications_updated_at ON external_applications(updated_at DESC);
