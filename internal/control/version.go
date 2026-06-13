package control

// Version is the Supadupa platform release version. Keep this in sync with
// frontend/package.json.
const Version = "0.2.0"

// BuildSHA is the git commit the binary was built from. It is a var so it can be
// stamped at build time via -ldflags "-X supadupa2026/internal/control.BuildSHA=...";
// it stays "unknown" for a plain `go build` with no ldflags.
var BuildSHA = "unknown"
