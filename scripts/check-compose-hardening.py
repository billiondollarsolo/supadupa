#!/usr/bin/env python3
"""Validate Docker Compose hardening invariants for platform deployment files."""

from __future__ import annotations

import json
import os
import subprocess
from collections.abc import Iterable


REQUIRED_ENV = {
    "SUPADUPA_SECRET_KEY": "a" * 64,
    "SUPADUPA_AUTH_SECRET": "b" * 64,
}


def main() -> int:
    env = os.environ.copy()
    env.update({key: env.get(key, value) for key, value in REQUIRED_ENV.items()})

    base = compose_config_json(env, "deploy/compose.yaml")
    apply = compose_config_json(env, "deploy/compose.yaml", "deploy/compose.apply.yaml")

    assert_base_compose(base)
    assert_apply_compose(apply)
    print("compose hardening check passed")
    return 0


def compose_config_json(env: dict[str, str], *files: str) -> dict:
    command = ["docker", "compose"]
    for file in files:
        command.extend(["-f", file])
    command.extend(["config", "--format", "json"])
    result = subprocess.run(command, check=True, capture_output=True, text=True, env=env)
    return json.loads(result.stdout)


def assert_base_compose(config: dict) -> None:
    services = config.get("services") or {}
    socket_owners = docker_socket_owners(config)
    if socket_owners != ["docker-socket-proxy"]:
        raise SystemExit(
            "base compose config may only mount the Docker socket via docker-socket-proxy, "
            f"got: {socket_owners!r}"
        )

    for name, service in services.items():
        command = service.get("command") or []
        if isinstance(command, str):
            command = [command]
        if any("providers.docker" in str(argument) for argument in command):
            raise SystemExit(f"base compose service {name!r} must not enable the Traefik Docker provider")

        docker_labels = [label for label in label_names(service) if str(label).startswith("traefik.http.")]
        if docker_labels:
            raise SystemExit(f"base compose service {name!r} must not define Docker Traefik labels: {docker_labels!r}")


def assert_apply_compose(config: dict) -> None:
    services = config.get("services") or {}
    owners = docker_socket_owners(config)
    if owners != ["docker-socket-proxy"]:
        raise SystemExit(f"Docker socket mount owners mismatch: {owners!r}")

    visor = services.get("supadupavisor", {})
    environment = visor.get("environment") or {}
    if environment.get("SUPADUPA_COMPOSE_APPLY") != "true":
        raise SystemExit("apply overlay must enable SUPADUPA_COMPOSE_APPLY")
    if environment.get("DOCKER_HOST") != "tcp://docker-socket-proxy:2375":
        raise SystemExit("supadupavisor DOCKER_HOST does not point at proxy")

    proxy_networks = set((services.get("docker-socket-proxy", {}).get("networks") or {}).keys())
    if proxy_networks != {"supadupa-docker-proxy"}:
        raise SystemExit(f"docker-socket-proxy network mismatch: {proxy_networks!r}")
    network = (config.get("networks") or {}).get("supadupa-docker-proxy", {})
    if not network.get("internal"):
        raise SystemExit("docker proxy network must be internal")


def docker_socket_owners(config: dict) -> list[str]:
    owners = []
    for name, service in (config.get("services") or {}).items():
        for volume in service.get("volumes", []) or []:
            if docker_socket_volume(volume):
                owners.append(name)
    return owners


def docker_socket_volume(volume: object) -> bool:
    if isinstance(volume, dict):
        return volume.get("source") == "/var/run/docker.sock" or volume.get("target") == "/var/run/docker.sock"
    return isinstance(volume, str) and "/var/run/docker.sock" in volume


def label_names(service: dict) -> Iterable[str]:
    labels = service.get("labels") or {}
    if isinstance(labels, dict):
        return labels.keys()
    return [str(label).split("=", 1)[0] for label in labels]


if __name__ == "__main__":
    raise SystemExit(main())
