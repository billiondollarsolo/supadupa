-- The Compose provisioner now reports a transient "starting" phase for projects
-- that are booting or mid-recreate (a service running with its healthcheck still
-- in the start window). The reconciler persists that phase into projects.status,
-- so the status CHECK constraint must accept it; otherwise every checkpoint that
-- touches a starting project fails (which also fails unrelated writes like login).
ALTER TABLE projects DROP CONSTRAINT IF EXISTS projects_status_check;
ALTER TABLE projects
    ADD CONSTRAINT projects_status_check
    CHECK (status IN ('provisioning', 'starting', 'healthy', 'degraded', 'paused', 'error', 'destroying'));
