-- IOPS was never a measured or enforceable quantity: host capacity defaulted to
-- a hardcoded value and tier reservations carried fixed constants, so the
-- max_disk_iops quota column could not reflect anything real. The control plane
-- no longer models disk IOPS at all (HostCapacity/OrgQuota dropped the field and
-- billing dropped the line item), so retire the column. Host capacity and
-- per-project sizing live in JSON (hosts.capacity / projects.desired_state) and
-- need no schema change.
ALTER TABLE org_quotas DROP CONSTRAINT IF EXISTS org_quotas_max_disk_iops_check;
ALTER TABLE org_quotas DROP COLUMN IF EXISTS max_disk_iops;
