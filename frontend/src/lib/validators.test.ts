import { describe, expect, it } from "vitest";
import { isValidCIDR, isValidEmail, isValidProjectRef } from "./validators";

describe("isValidEmail", () => {
  it("accepts common email shapes", () => {
    expect(isValidEmail("user@example.com")).toBe(true);
    expect(isValidEmail("  a.b+tag@sub.domain.io  ")).toBe(true);
  });

  it("rejects empty and malformed values", () => {
    expect(isValidEmail("")).toBe(false);
    expect(isValidEmail("   ")).toBe(false);
    expect(isValidEmail("not-an-email")).toBe(false);
    expect(isValidEmail("@missing-local.com")).toBe(false);
    expect(isValidEmail("missing-domain@")).toBe(false);
    expect(isValidEmail("a@b")).toBe(false);
  });
});

describe("isValidProjectRef", () => {
  it("accepts refs matching backend projectRefPattern (3–55, no leading/trailing hyphen)", () => {
    expect(isValidProjectRef("abc")).toBe(true);
    expect(isValidProjectRef("my-project")).toBe(true);
    expect(isValidProjectRef("a1b2c3")).toBe(true);
    expect(isValidProjectRef("a-b")).toBe(true);
  });

  it("rejects invalid refs", () => {
    expect(isValidProjectRef("")).toBe(false);
    expect(isValidProjectRef("ab")).toBe(false); // too short (min 3)
    expect(isValidProjectRef("a")).toBe(false);
    expect(isValidProjectRef("-abc")).toBe(false);
    expect(isValidProjectRef("abc-")).toBe(false);
    expect(isValidProjectRef("ABC")).toBe(false); // uppercase
    expect(isValidProjectRef("has_underscore")).toBe(false);
    expect(isValidProjectRef("has space")).toBe(false);
  });

  it("enforces max length 55", () => {
    const ok55 = "a" + "b".repeat(53) + "c";
    expect(ok55.length).toBe(55);
    expect(isValidProjectRef(ok55)).toBe(true);

    const tooLong = "a" + "b".repeat(54) + "c";
    expect(tooLong.length).toBe(56);
    expect(isValidProjectRef(tooLong)).toBe(false);
  });
});

describe("isValidCIDR", () => {
  it("accepts IPv4 CIDRs and bare IPv4 addresses", () => {
    expect(isValidCIDR("10.0.0.0/8")).toBe(true);
    expect(isValidCIDR("192.168.1.0/24")).toBe(true);
    expect(isValidCIDR("0.0.0.0/0")).toBe(true);
    expect(isValidCIDR("255.255.255.255/32")).toBe(true);
    expect(isValidCIDR("203.0.113.10")).toBe(true); // single IP as /32 optional
    expect(isValidCIDR("  10.0.0.0/16  ")).toBe(true);
  });

  it("rejects empty, malformed, and out-of-range values", () => {
    expect(isValidCIDR("")).toBe(false);
    expect(isValidCIDR("   ")).toBe(false);
    expect(isValidCIDR("not-a-cidr")).toBe(false);
    expect(isValidCIDR("10.0.0.0/")).toBe(false);
    expect(isValidCIDR("10.0.0.0/33")).toBe(false);
    expect(isValidCIDR("10.0.0.0/-1")).toBe(false);
    expect(isValidCIDR("10.0.0.0/24abc")).toBe(false);
    expect(isValidCIDR("10.0.0/24")).toBe(false);
    expect(isValidCIDR("256.0.0.1/32")).toBe(false);
    expect(isValidCIDR("01.2.3.4/32")).toBe(false); // leading zero
    expect(isValidCIDR("2001:db8::/32")).toBe(false); // IPv6 out of scope
  });
});
