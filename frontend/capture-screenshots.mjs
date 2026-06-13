import { chromium } from "@playwright/test";
import fs from "node:fs";

const BASE = process.env.SD_BASE || "https://admin.supadupatest.brotechlabs.com";
const EMAIL = process.env.SD_EMAIL;
const PASSWORD = process.env.SD_PASSWORD;
const OUT_ROOT = process.env.SD_OUT || "/root/supadupa/screenshots";
const THEMES = (process.env.SD_THEMES || "dark,light").split(",").map((t) => t.trim()).filter(Boolean);

if (!EMAIL || !PASSWORD) {
  console.error("SD_EMAIL and SD_PASSWORD are required");
  process.exit(1);
}

const browser = await chromium.launch({
  executablePath:
    process.env.PLAYWRIGHT_CHROMIUM_EXECUTABLE_PATH || "/usr/bin/google-chrome-stable",
});

const BASE_H = 1080;

// Reset to a known viewport, let content + live data settle, then grow the
// viewport to the largest internal scroll overflow (the app shell is a
// fixed-height column with an inner overflow container, so fullPage alone clips
// long pages) and capture the full page at 2x.
async function shot(target, dir, name) {
  await target.setViewportSize({ width: 1920, height: BASE_H });
  await target.waitForLoadState("networkidle", { timeout: 4000 }).catch(() => {});
  await target.waitForTimeout(1800);
  const extra = await target.evaluate(() => {
    let max = document.documentElement.scrollHeight - window.innerHeight;
    for (const el of document.querySelectorAll("*")) {
      const oy = getComputedStyle(el).overflowY;
      if (oy === "auto" || oy === "scroll") max = Math.max(max, el.scrollHeight - el.clientHeight);
    }
    return Math.max(0, Math.ceil(max));
  });
  if (extra > 0) {
    await target.setViewportSize({ width: 1920, height: Math.min(BASE_H + extra, 24000) });
    await target.waitForTimeout(600);
  }
  await target.screenshot({ path: `${dir}/${name}.png`, fullPage: true });
  const { size } = fs.statSync(`${dir}/${name}.png`);
  console.log(`  captured ${name}.png (${Math.round(size / 1024)} KB)`);
}

const topPages = [
  ["/", "01-dashboard"],
  ["/projects", "02-projects"],
  ["/projects/new", "03-project-new"],
  ["/organizations", "04-access"],
  ["/hosts", "05-hosts"],
  ["/security", "06-security"],
  ["/audit", "07-audit"],
  ["/settings", "08-settings"],
  ["/about", "09-about"],
];
const projTabs = [
  ["", "10-project-overview"],
  ["/connect", "11-project-connect"],
  ["/access", "12-project-access"],
  ["/database", "13-project-database"],
  ["/logs", "14-project-logs"],
  ["/backups", "15-project-backups"],
  ["/config", "16-project-settings"],
  ["/activity", "17-project-activity"],
];

// Studio is the embedded Supabase Studio app on its own hostname; the
// session-code redirect sets a cookie on first load, so we then navigate to
// each section directly within the same popup. Paths are relative to the studio
// origin and use the self-hosted "default" project ref.
const studioPages = [
  ["/project/default/editor", "19-studio-table-editor"],
  ["/project/default/sql", "20-studio-sql-editor"],
  ["/project/default/database/schemas", "21-studio-database"],
  ["/project/default/auth/users", "22-studio-auth"],
  ["/project/default/storage/files", "23-studio-storage"],
  ["/project/default/functions", "24-studio-functions"],
  ["/project/default/realtime/inspector", "25-studio-realtime"],
  ["/project/default/advisors/security", "26-studio-advisors"],
  ["/project/default/logs/explorer", "27-studio-logs"],
  ["/project/default/integrations", "28-studio-integrations"],
  ["/project/default/settings/general", "29-studio-settings"],
];

const studioOnly = process.env.SD_ONLY === "studio";

// Studio shows a floating "What's new" banner (div.fixed.bottom-4.right-4) that
// overlaps page content. Dismiss it before capturing for clean shots.
async function dismissStudioBanner(p) {
  try {
    const btn = p.getByRole("button", { name: "Close banner" });
    if (await btn.count()) {
      await btn.first().click({ timeout: 1500 }).catch(() => {});
      await p.waitForTimeout(300);
    }
  } catch {}
}

async function captureTheme(theme) {
  const dir = `${OUT_ROOT}/${theme}`;
  fs.mkdirSync(dir, { recursive: true });
  console.log(`\n=== theme: ${theme} -> ${dir} ===`);

  const context = await browser.newContext({
    viewport: { width: 1920, height: BASE_H },
    deviceScaleFactor: 2, // 2x retina-quality output
    colorScheme: theme, // drives prefers-color-scheme for Studio + any media queries
  });
  // Set the app theme before any page script runs, on every navigation.
  await context.addInitScript((t) => {
    try {
      localStorage.setItem("supadupa-theme", t);
      if (document.documentElement) {
        document.documentElement.dataset.theme = t;
      }
    } catch {}
  }, theme);

  const page = await context.newPage();
  page.setDefaultTimeout(20000);

  // --- login page (pre-auth) ---
  await page.goto(`${BASE}/login`, { waitUntil: "domcontentloaded" });
  await page.getByPlaceholder("admin@example.com").waitFor();
  await page.evaluate((t) => {
    document.documentElement.dataset.theme = t;
  }, theme);
  await shot(page, dir, "00-login");

  // --- log in ---
  await page.getByPlaceholder("admin@example.com").fill(EMAIL);
  await page.getByPlaceholder("Password", { exact: true }).fill(PASSWORD);
  await page.getByRole("button", { name: "Login", exact: true }).click();
  await page.getByRole("heading", { name: "Dashboard" }).waitFor();
  console.log("  logged in");

  // --- top-level pages ---
  if (!studioOnly) {
    for (const [path, name] of topPages) {
      await page.goto(`${BASE}${path}`, { waitUntil: "domcontentloaded" });
      await shot(page, dir, name);
    }
  }

  // --- discover a project ref ---
  await page.goto(`${BASE}/projects`, { waitUntil: "domcontentloaded" });
  await page.waitForTimeout(1500);
  const refs = await page.$$eval('a[href^="/projects/"]', (els) => [
    ...new Set(
      els
        .map((e) => e.getAttribute("href"))
        .filter((h) => h && /^\/projects\/[^/]+$/.test(h) && h !== "/projects/new")
        .map((h) => h.split("/")[2]),
    ),
  ]);
  if (process.env.SD_REF) refs.unshift(process.env.SD_REF);
  console.log("  project refs:", refs);

  if (refs.length) {
    const ref = refs[0];
    if (!studioOnly) {
      for (const [suffix, name] of projTabs) {
        await page.goto(`${BASE}/projects/${ref}${suffix}`, { waitUntil: "domcontentloaded" });
        await shot(page, dir, name);
      }
    }

    // --- studio: open the popup via the session-code redirect, then walk every
    // Studio section (the auth cookie persists across same-origin navigations) ---
    try {
      await page.goto(`${BASE}/projects/${ref}`, { waitUntil: "domcontentloaded" });
      await page.waitForTimeout(1500);
      const [popup] = await Promise.all([
        page.waitForEvent("popup", { timeout: 15000 }),
        page.getByRole("link", { name: /Open Studio/i }).click(),
      ]);
      popup.setDefaultTimeout(30000);
      await popup.waitForLoadState("domcontentloaded");
      await popup.waitForURL(/studio-/, { timeout: 15000 }).catch(() => {});
      await popup.waitForTimeout(3500);
      const sorigin = new URL(popup.url()).origin;
      await dismissStudioBanner(popup);
      await shot(popup, dir, "18-studio-home");
      for (const [path, name] of studioPages) {
        await popup.goto(`${sorigin}${path}`, { waitUntil: "domcontentloaded" }).catch(() => {});
        await popup.waitForTimeout(2500);
        await dismissStudioBanner(popup);
        await shot(popup, dir, name);
      }
      await popup.close();
    } catch (err) {
      console.warn("  studio capture failed:", err?.message || err);
    }
  } else {
    console.warn("  no project ref found — skipping project tabs + studio");
  }

  await context.close();
}

for (const theme of THEMES) {
  await captureTheme(theme);
}

await browser.close();
console.log("\ndone");
