#!/usr/bin/env python3
import sys
from pathlib import Path

import yaml


ROOT = Path(__file__).resolve().parents[1]
PROJECT_CRD = ROOT / "charts" / "supadupa" / "crds" / "projects.yaml"
PROJECT_REPLICA_CRD = ROOT / "charts" / "supadupa" / "crds" / "projectreplicas.yaml"
PROJECT_CONFIG_CRD = ROOT / "charts" / "supadupa" / "crds" / "projectconfigs.yaml"
PROJECT_AUTH_HOOKS_CRD = ROOT / "charts" / "supadupa" / "crds" / "projectauthhooks.yaml"
PROJECT_BRANCH_CLONE_CRD = ROOT / "charts" / "supadupa" / "crds" / "projectbranchclones.yaml"
RETAINED_PROJECT_RESOURCES_CRD = ROOT / "charts" / "supadupa" / "crds" / "retainedprojectresources.yaml"


def fail(message: str) -> None:
    print(message, file=sys.stderr)
    raise SystemExit(1)


def nested(mapping: dict, *keys: str) -> dict:
    current = mapping
    for key in keys:
        if not isinstance(current, dict) or key not in current:
            fail(f"missing CRD schema path: {'.'.join(keys)}")
        current = current[key]
    if not isinstance(current, dict):
        fail(f"CRD schema path is not an object: {'.'.join(keys)}")
    return current


def assert_type(mapping: dict, expected: str, path: str) -> None:
    actual = mapping.get("type")
    if actual != expected:
        fail(f"{path} type is {actual!r}, expected {expected!r}")


def main() -> None:
    crd = yaml.safe_load(PROJECT_CRD.read_text())
    versions = nested(crd, "spec").get("versions")
    if not isinstance(versions, list) or not versions:
        fail("Project CRD spec.versions must be a non-empty list")
    schema = versions[0]["schema"]["openAPIV3Schema"]
    spec = nested(schema, "properties", "spec")
    status = nested(schema, "properties", "status")

    if spec.get("x-kubernetes-preserve-unknown-fields"):
        fail("Project spec must be structural, not preserve unknown fields")
    assert_type(nested(spec, "properties", "orgId"), "string", "spec.orgId")
    assert_type(nested(spec, "properties", "displayName"), "string", "spec.displayName")
    assert_type(nested(spec, "properties", "hostId"), "string", "spec.hostId")
    assert_type(nested(spec, "properties", "services"), "object", "spec.services")
    assert_type(nested(spec, "properties", "services", "additionalProperties"), "object", "spec.services.*")
    assert_type(nested(spec, "properties", "services", "additionalProperties", "properties", "ports"), "array", "spec.services.*.ports")
    service_properties = nested(spec, "properties", "services", "additionalProperties", "properties")
    assert_type(nested(service_properties, "command"), "array", "spec.services.*.command")
    assert_type(nested(service_properties, "command", "items"), "string", "spec.services.*.command[]")
    assert_type(nested(service_properties, "args"), "array", "spec.services.*.args")
    assert_type(nested(service_properties, "args", "items"), "string", "spec.services.*.args[]")
    port = nested(spec, "properties", "services", "additionalProperties", "properties", "ports", "items", "properties", "port")
    if port.get("minimum") != 1 or port.get("maximum") != 65535:
        fail("spec.services.*.ports[].port must be constrained to 1..65535")
    assert_type(nested(service_properties, "dependsOn"), "array", "spec.services.*.dependsOn")
    dependency_port = nested(service_properties, "dependsOn", "items", "properties", "port")
    if dependency_port.get("minimum") != 1 or dependency_port.get("maximum") != 65535:
        fail("spec.services.*.dependsOn[].port must be constrained to 1..65535")
    assert_type(nested(service_properties, "runAsNonRoot"), "boolean", "spec.services.*.runAsNonRoot")
    assert_type(nested(service_properties, "allowPrivilegeEscalation"), "boolean", "spec.services.*.allowPrivilegeEscalation")
    assert_type(nested(service_properties, "dropCapabilities"), "array", "spec.services.*.dropCapabilities")
    assert_type(nested(service_properties, "dropCapabilities", "items"), "string", "spec.services.*.dropCapabilities[]")
    assert_type(nested(service_properties, "readOnlyRootFilesystem"), "boolean", "spec.services.*.readOnlyRootFilesystem")
    assert_type(nested(service_properties, "configFiles"), "array", "spec.services.*.configFiles")
    assert_type(nested(service_properties, "configFiles", "items", "properties", "mountPath"), "string", "spec.services.*.configFiles[].mountPath")
    assert_type(nested(service_properties, "configFiles", "items", "properties", "content"), "string", "spec.services.*.configFiles[].content")
    assert_type(nested(service_properties, "writablePaths"), "array", "spec.services.*.writablePaths")
    assert_type(nested(service_properties, "writablePaths", "items", "properties", "mountPath"), "string", "spec.services.*.writablePaths[].mountPath")
    assert_type(nested(service_properties, "readinessProbe", "properties", "port"), "integer", "spec.services.*.readinessProbe.port")
    assert_type(nested(service_properties, "livenessProbe", "properties", "port"), "integer", "spec.services.*.livenessProbe.port")
    assert_type(nested(spec, "properties", "runtimeSecurityDefaults", "properties", "allowPrivilegeEscalation"), "boolean", "spec.runtimeSecurityDefaults.allowPrivilegeEscalation")
    assert_type(nested(spec, "properties", "environment", "additionalProperties"), "string", "spec.environment values")
    assert_type(nested(status, "properties", "conditions"), "array", "status.conditions")

    replica_crd = yaml.safe_load(PROJECT_REPLICA_CRD.read_text())
    replica_versions = nested(replica_crd, "spec").get("versions")
    if not isinstance(replica_versions, list) or not replica_versions:
        fail("ProjectReplica CRD spec.versions must be a non-empty list")
    replica_schema = replica_versions[0]["schema"]["openAPIV3Schema"]
    replica_spec = nested(replica_schema, "properties", "spec")
    if replica_spec.get("x-kubernetes-preserve-unknown-fields"):
        fail("ProjectReplica spec must be structural, not preserve unknown fields")
    assert_type(nested(replica_spec, "properties", "projectRef"), "string", "projectreplica.spec.projectRef")
    assert_type(nested(replica_spec, "properties", "runtimeSecurityDefaults", "properties", "allowPrivilegeEscalation"), "boolean", "projectreplica.spec.runtimeSecurityDefaults.allowPrivilegeEscalation")
    assert_type(nested(replica_spec, "properties", "runtimeSecurityDefaults", "properties", "dropCapabilities"), "array", "projectreplica.spec.runtimeSecurityDefaults.dropCapabilities")

    project_config_spec = crd_spec_schema(PROJECT_CONFIG_CRD, "ProjectConfig")
    assert_structural_spec(project_config_spec, "ProjectConfig")
    assert_type(nested(project_config_spec, "properties", "projectRef"), "string", "projectconfig.spec.projectRef")
    assert_type(nested(project_config_spec, "properties", "area"), "string", "projectconfig.spec.area")
    assert_type(nested(project_config_spec, "properties", "config", "additionalProperties"), "string", "projectconfig.spec.config values")

    auth_hooks_spec = crd_spec_schema(PROJECT_AUTH_HOOKS_CRD, "ProjectAuthHooks")
    assert_structural_spec(auth_hooks_spec, "ProjectAuthHooks")
    assert_type(nested(auth_hooks_spec, "properties", "projectRef"), "string", "projectauthhooks.spec.projectRef")
    assert_type(nested(auth_hooks_spec, "properties", "hooks"), "array", "projectauthhooks.spec.hooks")
    assert_type(nested(auth_hooks_spec, "properties", "hooks", "items", "properties", "headers", "additionalProperties"), "string", "projectauthhooks.spec.hooks[].headers values")
    assert_type(nested(auth_hooks_spec, "properties", "hooks", "items", "properties", "timeoutMS"), "integer", "projectauthhooks.spec.hooks[].timeoutMS")

    branch_clone_spec = crd_spec_schema(PROJECT_BRANCH_CLONE_CRD, "ProjectBranchClone")
    assert_structural_spec(branch_clone_spec, "ProjectBranchClone")
    assert_type(nested(branch_clone_spec, "properties", "sourceRef"), "string", "projectbranchclone.spec.sourceRef")
    assert_type(nested(branch_clone_spec, "properties", "branchRef"), "string", "projectbranchclone.spec.branchRef")

    retained_spec = crd_spec_schema(RETAINED_PROJECT_RESOURCES_CRD, "RetainedProjectResources")
    assert_structural_spec(retained_spec, "RetainedProjectResources")
    assert_type(nested(retained_spec, "properties", "projectRef"), "string", "retainedprojectresources.spec.projectRef")
    assert_type(nested(retained_spec, "properties", "resources"), "array", "retainedprojectresources.spec.resources")
    assert_type(nested(retained_spec, "properties", "resources", "items", "properties", "name"), "string", "retainedprojectresources.spec.resources[].name")


def crd_spec_schema(path: Path, kind: str) -> dict:
    crd = yaml.safe_load(path.read_text())
    versions = nested(crd, "spec").get("versions")
    if not isinstance(versions, list) or not versions:
        fail(f"{kind} CRD spec.versions must be a non-empty list")
    schema = versions[0]["schema"]["openAPIV3Schema"]
    return nested(schema, "properties", "spec")


def assert_structural_spec(spec: dict, kind: str) -> None:
    if spec.get("x-kubernetes-preserve-unknown-fields"):
        fail(f"{kind} spec must be structural, not preserve unknown fields")
    assert_type(spec, "object", f"{kind}.spec")


if __name__ == "__main__":
    main()
