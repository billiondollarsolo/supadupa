package control

import "sync/atomic"

// Process-local counters for legacy credential / MFA migration pressure.
// Incremented when a legacy verify or load path is used successfully (password /
// SCIM) or when a legacy plaintext MFA seed is read from normalized storage.
// Exposed on GET /metrics as counters; also available to unit tests via the
// Count accessors.

var (
	legacyPasswordHashVerifyTotal atomic.Uint64
	legacySCIMHashVerifyTotal     atomic.Uint64
	legacyMFAPlaintextLoadTotal   atomic.Uint64
)

func noteLegacyPasswordHashVerify() {
	legacyPasswordHashVerifyTotal.Add(1)
}

func noteLegacySCIMHashVerify() {
	legacySCIMHashVerifyTotal.Add(1)
}

func noteLegacyMFAPlaintextLoad() {
	legacyMFAPlaintextLoadTotal.Add(1)
}

// LegacyPasswordHashVerifyCount is the number of successful password
// verifications that used a legacy sha256$ hash since process start.
func LegacyPasswordHashVerifyCount() uint64 {
	return legacyPasswordHashVerifyTotal.Load()
}

// LegacySCIMHashVerifyCount is the number of successful SCIM token
// verifications that used a legacy unkeyed SHA-256 hash since process start.
func LegacySCIMHashVerifyCount() uint64 {
	return legacySCIMHashVerifyTotal.Load()
}

// LegacyMFAPlaintextLoadCount is the number of times a normalized MFA seed
// column was loaded as legacy plaintext (no application envelope) since process start.
func LegacyMFAPlaintextLoadCount() uint64 {
	return legacyMFAPlaintextLoadTotal.Load()
}
