package control

import "strings"

// PinImage returns repository:tag@sha256:... when digest is set, otherwise repository:tag.
// Digests must be multi-arch index digests (docker buildx imagetools inspect Digest line).
func PinImage(repository string, tag string, digest string) string {
	repository = strings.TrimSpace(repository)
	tag = strings.TrimSpace(tag)
	if repository == "" {
		return ""
	}
	base := repository
	if tag != "" {
		base = repository + ":" + tag
	}
	digest = strings.TrimSpace(digest)
	if digest == "" {
		return base
	}
	if !strings.HasPrefix(digest, "sha256:") {
		digest = "sha256:" + digest
	}
	// Docker accepts name:tag@sha256:... (tag kept for operator readability).
	return base + "@" + digest
}

// ImageRef returns the digest-pinned image reference for a stack service when the
// manifest carries digests (plan B3). service keys match stack release digests map
// keys: postgres, kong, studio, postgres_meta, auth, rest, realtime, storage,
// imgproxy, edge_runtime, pooler, analytics, vector.
func (m StackReleaseManifest) ImageRef(service string, repository string, tag string) string {
	digest := ""
	if m.Digests != nil {
		digest = m.Digests[service]
	}
	return PinImage(repository, tag, digest)
}

// builtinStackServiceDigests holds shared non-postgres service digests used by all
// built-in releases that ship the same component tags.
var builtinStackServiceDigests = map[string]string{
	"analytics": "sha256:a0c4323d57ada22270d740796cbd2c353cd5c627479dff075c0f0bbb3fa5e5b0",
	"auth": "sha256:385184459f57569c54c25209f51f3b2be99ddd7c4ce9e3555b5d3eea8447b7cf",
	"edge_runtime": "sha256:2781daf92394db91f7e94129cc3d04ec474ad16a8fe64b3fbeef6e7d557ab120",
	"imgproxy": "sha256:3b709e4a0e5e8e0e959b556b7031229202b4b8e7e7d955c517ea7abed68ee34d",
	"kong": "sha256:6addf50e6bd8d578314cb9ce4f2d2d1e3781d2edecef59f707e00c6e05d384f5",
	"pooler": "sha256:31c2f05b13b11069660fdfae2f6cfd37b509748d2710aca121cfee8b16cb8b07",
	"postgres_meta": "sha256:a84cc713585eea7b401e4a2561ec4a1e48c87083d1c7ecb4502f204bb4391300",
	"realtime": "sha256:aa1c92c0cf326007563641730ec9da9c60478caa6853887775365fa2c097a471",
	"rest": "sha256:54000f24847d01a2c2302e0041cf0618b875c57fb48507d743cfa9aaa50bf43c",
	"storage": "sha256:c8eb9858eafec891a97c27125470aaad54703c3f4eb4d55ca7f1bf6c6411febf",
	"studio": "sha256:202bef35951293fb97d658f4cc849552a70915fe89d0426a8a5e63846a6506f2",
	"vector": "sha256:ca92d617e905953c3f852e7e88061f7039460e733522e3f0c21bc6ae946b2558",
}

func stackDigestsForPostgresVersion(version string) map[string]string {
	out := make(map[string]string, len(builtinStackServiceDigests)+1)
	for k, v := range builtinStackServiceDigests {
		out[k] = v
	}
	switch version {
	case "15.8.1.085":
		out["postgres"] = "sha256:af083ef64d0408c8f098ee6f5c364a59b26f36fbc0f3a334a62c5c1d57362e9b"
	case "15.8.1.060":
		out["postgres"] = "sha256:0e2279598bc0224fb5960c3a61eb23270cd60119427f3a7bdec86ba282600dcc"
	case "15.8.1.054":
		out["postgres"] = "sha256:7b2b3f2b995c6f257581725bdca7b4d6159af85ae878524ed78f3ce9d07d883d"
	case "15.8.1.049":
		out["postgres"] = "sha256:76cadf4b121eb0207b3434878e02369b1db148f3e05e027256ba6c9a59bee56c"
	}
	return out
}
