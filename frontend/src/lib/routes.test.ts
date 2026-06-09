import { describe, expect, it } from "vitest";
import {
  projectPath,
  projectRefFromPathname,
  projectSectionFromPathname,
  projectSubrouteFromPathname,
  projectTabFromPathname,
} from "./routes";

describe("project route helpers", () => {
  it("encodes dynamic project route segments", () => {
    expect(projectPath("org/project ref", "database", "replicas", "assets/2026%raw")).toBe(
      "/projects/org%2Fproject%20ref/database/replicas/assets%2F2026%25raw",
    );
  });

  it("decodes project refs, sections, and item ids from pathnames", () => {
    const pathname = projectPath("org/project ref", "database", "replicas", "assets/2026%raw");

    expect(projectRefFromPathname(pathname)).toBe("org/project ref");
    expect(projectTabFromPathname(pathname)).toBe("database");
    expect(projectSectionFromPathname(pathname, "database")).toBe("replicas");
    expect(projectSubrouteFromPathname(pathname, "database")).toEqual({
      section: "replicas",
      item: "assets/2026%raw",
    });
  });

  it("keeps unmatched tab subroutes on the overview fallback", () => {
    expect(projectSubrouteFromPathname(projectPath("demo", "logs", "drains", "new"), "database")).toEqual({
      section: "overview",
      item: "",
    });
  });

  it("falls back safely for non-project and malformed encoded paths", () => {
    expect(projectRefFromPathname("/settings")).toBe("");
    expect(projectRefFromPathname("/projects/%E0%A4%A")).toBe("");
    expect(projectTabFromPathname("/projects/demo/%E0%A4%A")).toBe("overview");
    expect(projectSubrouteFromPathname("/projects/demo/database/%E0%A4%A/new", "database")).toEqual({
      section: "overview",
      item: "new",
    });
  });
});
