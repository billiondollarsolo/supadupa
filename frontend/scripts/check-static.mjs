import { readdirSync, readFileSync } from "node:fs";
import { join } from "node:path";

const apiSource = readFileSync(new URL("../src/api.ts", import.meta.url), "utf8");
const authSources = [
  ["src/api.ts", apiSource],
  ["src/lib/auth-session.ts", readFileSync(new URL("../src/lib/auth-session.ts", import.meta.url), "utf8")],
  ["src/pages/login.tsx", readFileSync(new URL("../src/pages/login.tsx", import.meta.url), "utf8")],
  ["src/components/runtime-link.tsx", readFileSync(new URL("../src/components/runtime-link.tsx", import.meta.url), "utf8")],
];

const failures = [];

failures.push(...verifyStaticCheckHelpers());
failures.push(...apiPathInterpolationFailures(apiSource, "src/api.ts"));

for (const path of walk(new URL("../src", import.meta.url))) {
  const source = readFileSync(path, "utf8");
  for (const forbidden of ["supadupa_token", "supadupa_studio_token"]) {
    if (source.includes(forbidden)) {
      failures.push(`${relativeSourcePath(path)}: forbidden browser token storage or URL token marker ${forbidden}`);
    }
  }
}

for (const [path, source] of authSources) {
  for (const forbidden of ["localStorage", "sessionStorage"]) {
    if (source.includes(forbidden)) {
      failures.push(`${path}: auth-sensitive browser session code must not use ${forbidden}`);
    }
  }
}

for (const path of walk(new URL("../src", import.meta.url))) {
  if (!path.endsWith(".tsx")) {
    continue;
  }
  const source = readFileSync(path, "utf8");
  if (source.includes("dangerouslySetInnerHTML")) {
    failures.push(`${relativeSourcePath(path)}: dangerouslySetInnerHTML requires an explicit security review and sanitizer`);
  }
  source.split("\n").forEach((line, index) => {
    if (line.includes("/projects/${") || line.includes("`/projects/")) {
      failures.push(`${relativeSourcePath(path)}:${index + 1}: project routes must use projectPath() or route params`);
    }
  });
}

if (failures.length > 0) {
  console.error(failures.join("\n"));
  process.exit(1);
}

console.log("frontend static remediation checks passed");

function walk(url) {
  const root = url.pathname;
  const out = [];
  const stack = [root];
  while (stack.length > 0) {
    const current = stack.pop();
    for (const entry of readdirSync(current, { withFileTypes: true })) {
      const next = join(current, entry.name);
      if (entry.isDirectory()) {
        stack.push(next);
      } else {
        out.push(next);
      }
    }
  }
  return out;
}

function relativeSourcePath(path) {
  const marker = "/frontend/";
  const index = path.indexOf(marker);
  return index === -1 ? path : path.slice(index + marker.length);
}

function verifyStaticCheckHelpers() {
  const fixture = [
    "request(`/v1/projects/${ref}/logs`);",
    "request(`/v1/projects/${",
    "  ref",
    "}/logs`);",
    "fetch(`${apiBase}/v1/projects/${segment(ref)}/logs?stream=true`);",
  ].join("\n");
  const got = apiPathInterpolationFailures(fixture, "fixture.ts");
  const selfFailures = [];
  if (got.length !== 2) {
    selfFailures.push(`frontend static helper self-test expected 2 path failures, got ${got.length}`);
  }
  if (!got.some((failure) => failure.includes("fixture.ts:1") && failure.includes("ref"))) {
    selfFailures.push("frontend static helper self-test did not catch one-line raw /v1 interpolation");
  }
  if (!got.some((failure) => failure.includes("fixture.ts:2") && failure.includes("ref"))) {
    selfFailures.push("frontend static helper self-test did not catch multi-line raw /v1 interpolation");
  }
  if (got.some((failure) => failure.includes("apiBase"))) {
    selfFailures.push("frontend static helper self-test rejected allowed apiBase-prefixed /v1 URL");
  }
  return selfFailures;
}

function apiPathInterpolationFailures(source, path) {
  const out = [];
  for (const literal of templateLiterals(source)) {
    if (!literal.value.includes("/v1")) {
      continue;
    }
    const apiPathIndex = literal.value.indexOf("/v1");
    for (const expression of templateExpressions(literal.value)) {
      const value = expression.value.trim();
      if (expression.index < apiPathIndex) {
        if (value !== "apiBase") {
          out.push(`${path}:${literal.line}: raw /v1 base interpolation must be apiBase: ${value}`);
        }
        continue;
      }
      if (!value.startsWith("segment(") && !value.startsWith("queryString(")) {
        out.push(`${path}:${literal.line}: raw /v1 interpolation must use segment() or queryString(): ${value}`);
      }
    }
  }
  return out;
}

function templateLiterals(source) {
  const out = [];
  let start = -1;
  let escaped = false;
  for (let index = 0; index < source.length; index += 1) {
    const char = source[index];
    if (start === -1) {
      if (char === "`") {
        start = index;
      }
      continue;
    }
    if (escaped) {
      escaped = false;
      continue;
    }
    if (char === "\\") {
      escaped = true;
      continue;
    }
    if (char === "`") {
      out.push({
        value: source.slice(start, index + 1),
        line: lineNumber(source, start),
      });
      start = -1;
    }
  }
  return out;
}

function templateExpressions(literal) {
  const expressions = [];
  let index = 0;
  while (index < literal.length) {
    const start = literal.indexOf("${", index);
    if (start === -1) {
      break;
    }
    let depth = 1;
    let cursor = start + 2;
    while (cursor < literal.length && depth > 0) {
      const char = literal[cursor];
      if (char === "{") {
        depth += 1;
      } else if (char === "}") {
        depth -= 1;
      }
      cursor += 1;
    }
    if (depth === 0) {
      expressions.push({
        index: start,
        value: literal.slice(start + 2, cursor - 1),
      });
    }
    index = cursor;
  }
  return expressions;
}

function lineNumber(source, index) {
  let line = 1;
  for (let cursor = 0; cursor < index; cursor += 1) {
    if (source[cursor] === "\n") {
      line += 1;
    }
  }
  return line;
}
