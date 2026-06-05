CREATE TABLE function_storage_mounts (
    id UUID PRIMARY KEY,
    project_id UUID NOT NULL REFERENCES projects(id) ON DELETE CASCADE,
    function_name TEXT NOT NULL,
    bucket_name TEXT NOT NULL,
    mount_path TEXT NOT NULL,
    read_only BOOLEAN NOT NULL DEFAULT true,
    prefix TEXT,
    env_alias TEXT,
    status TEXT NOT NULL,
    message TEXT,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE (project_id, function_name, mount_path)
);

CREATE INDEX function_storage_mounts_project_id_idx ON function_storage_mounts(project_id);
CREATE INDEX function_storage_mounts_function_name_idx ON function_storage_mounts(project_id, function_name);
