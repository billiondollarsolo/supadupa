import { describe, expect, it } from "vitest";
import { isValidEmail, isValidProjectRef } from "./validators";

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
