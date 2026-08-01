/**
 * Shared client-side validators for form inputs.
 * Project ref rules mirror internal/control/store.go projectRefPattern:
 *   ^[a-z0-9](?:[a-z0-9-]{1,53}[a-z0-9])$
 * i.e. 3–55 lowercase alnum/hyphen, cannot start or end with a hyphen.
 */

const EMAIL_PATTERN = /^[^\s@]+@[^\s@]+\.[^\s@]+$/;

/** Backend projectRefPattern — 3–55 chars, start/end alnum, middle may include hyphens. */
const PROJECT_REF_PATTERN = /^[a-z0-9](?:[a-z0-9-]{1,53}[a-z0-9])$/;

/** Lightweight email shape check (not full RFC 5322). */
export function isValidEmail(value: string): boolean {
  const trimmed = value.trim();
  if (!trimmed || trimmed.length > 254) {
    return false;
  }
  return EMAIL_PATTERN.test(trimmed);
}

/**
 * Project ref: lowercase letters, numbers, hyphens; length 3–55;
 * cannot start or end with a hyphen (matches control.projectRefPattern).
 */
export function isValidProjectRef(value: string): boolean {
  return PROJECT_REF_PATTERN.test(value.trim());
}
