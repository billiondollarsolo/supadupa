/**
 * Shared client-side validators for form inputs.
 * Project ref rules mirror internal/control/store.go projectRefPattern:
 *   ^[a-z0-9](?:[a-z0-9-]{1,53}[a-z0-9])$
 * i.e. 3–55 lowercase alnum/hyphen, cannot start or end with a hyphen.
 *
 * CIDR rules align with control.normalizeNetworkCIDRs (netip.ParsePrefix /
 * ParseAddr): IPv4 CIDR or bare IPv4 address (optional /32).
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

/**
 * IPv4 CIDR or bare IPv4 address.
 * Accepts e.g. 10.0.0.0/8, 192.168.1.0/24, 203.0.113.10 (single IP as /32 optional).
 */
export function isValidCIDR(value: string): boolean {
  const trimmed = value.trim();
  if (!trimmed) {
    return false;
  }

  const slash = trimmed.indexOf("/");
  const ipPart = slash === -1 ? trimmed : trimmed.slice(0, slash);
  const prefixPart = slash === -1 ? null : trimmed.slice(slash + 1);

  if (!isValidIPv4(ipPart)) {
    return false;
  }

  // Bare IPv4 is allowed (backend treats ParseAddr as valid allowlist entry).
  if (prefixPart === null) {
    return true;
  }

  // Prefix must be decimal 0–32 with no leading junk.
  if (!/^\d{1,2}$/.test(prefixPart)) {
    return false;
  }
  const prefix = Number(prefixPart);
  return prefix >= 0 && prefix <= 32;
}

function isValidIPv4(value: string): boolean {
  const octets = value.split(".");
  if (octets.length !== 4) {
    return false;
  }
  return octets.every((octet) => {
    // Reject empty, non-digits, and leading zeros (except "0").
    if (!/^\d{1,3}$/.test(octet)) {
      return false;
    }
    if (octet.length > 1 && octet.startsWith("0")) {
      return false;
    }
    const n = Number(octet);
    return n >= 0 && n <= 255;
  });
}
