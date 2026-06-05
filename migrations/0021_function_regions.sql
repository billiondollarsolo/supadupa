CREATE TABLE function_regions (
    id UUID PRIMARY KEY,
    project_id UUID NOT NULL REFERENCES projects(id) ON DELETE CASCADE,
    function_name TEXT NOT NULL,
    host_id UUID REFERENCES hosts(id),
    region TEXT NOT NULL,
    routing_policy TEXT NOT NULL,
    invocation_url TEXT NOT NULL,
    status TEXT NOT NULL,
    message TEXT,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE (project_id, function_name, region)
);

CREATE INDEX function_regions_project_id_idx ON function_regions(project_id);
CREATE INDEX function_regions_function_name_idx ON function_regions(project_id, function_name);
CREATE INDEX function_regions_host_id_idx ON function_regions(host_id);
