import { expect, test, type Page } from "@playwright/test";

const live = {
  adminURL: process.env.SUPADUPA_LIVE_ADMIN_URL?.trim() || "",
  email: process.env.SUPADUPA_LIVE_EMAIL?.trim() || "",
  password: process.env.SUPADUPA_LIVE_PASSWORD || "",
  ref: process.env.SUPADUPA_LIVE_PROJECT_REF?.trim() || "",
  apiURL: process.env.SUPADUPA_LIVE_PROJECT_API_URL?.trim() || "",
  studioURL: process.env.SUPADUPA_LIVE_PROJECT_STUDIO_URL?.trim() || "",
  storageS3URL: process.env.SUPADUPA_LIVE_PROJECT_STORAGE_S3_URL?.trim() || "",
};

const hasLiveAdminConfig = Object.values(live).every((value) => value.length > 0);

test.skip(!hasLiveAdminConfig, "live Compose admin UI smoke requires SUPADUPA_LIVE_* environment variables");

test("manages a live Compose project through the admin UI without browser token storage", async ({ page }) => {
  await page.goto("/login");

  await expect(page.getByRole("heading", { name: "Admin login" })).toBeVisible();
  await expect(page.getByText("Management API online at")).toBeVisible();
  await page.getByPlaceholder("admin@example.com").fill(live.email);
  await page.getByPlaceholder("Password").fill(live.password);
  await page.getByRole("button", { name: "Login" }).click();

  await expect(page.getByRole("heading", { name: "Dashboard" })).toBeVisible();
  await assertNoBrowserAuthTokens(page);

  await page.getByRole("button", { name: new RegExp(`Admin UI Smoke Project.*${live.ref}`) }).click();
  await expect(page.getByRole("heading", { name: "Connect" })).toBeVisible();
  await page.getByRole("button", { name: /Full connect/ }).click();
  await expect(page.getByRole("heading", { name: "Connect" })).toBeVisible();
  await expect(page.getByRole("heading", { name: live.ref })).toBeVisible();
  await page.getByRole("button", { name: /Endpoints/ }).click();
  await expect(page.getByText(live.apiURL, { exact: true })).toBeVisible();
  await expect(page.getByText(`${live.apiURL}/rest/v1`, { exact: true })).toBeVisible();
  await expect(page.getByText(live.storageS3URL, { exact: true })).toBeVisible();

  await page.locator(".project-subnav").getByRole("link", { name: "Overview" }).click();
  await page.getByRole("button", { name: /Links/ }).click();
  await expect(page.getByText(live.studioURL, { exact: true }).first()).toBeVisible();

  await page.evaluate(() => {
    const openedURLs: string[] = [];
    Object.defineProperty(window, "__supadupaOpenedURLs", {
      configurable: true,
      value: openedURLs,
    });
    window.open = (() => ({
      close() {},
      location: {
        replace(nextURL: string) {
          openedURLs.push(String(nextURL));
        },
      },
      opener: null,
    })) as unknown as typeof window.open;
  });
  await page.locator("a", { hasText: "Studio" }).first().click();
  await expect.poll(() => openedStudioURL(page), { timeout: 10_000 }).toContain(live.studioURL);
  const studioURL = await openedStudioURL(page);
  expect(studioURL).toContain("supadupa_studio_code=");
  expect(studioURL).not.toMatch(/(?:access_)?token=|bearer|jwt/i);

  await assertNoBrowserAuthTokens(page);
});

async function openedStudioURL(page: Page) {
  return page.evaluate(() => {
    const openedURLs = (window as Window & { __supadupaOpenedURLs?: string[] }).__supadupaOpenedURLs ?? [];
    return openedURLs[0] ?? "";
  });
}

async function assertNoBrowserAuthTokens(page: Page) {
  const leaked = await page.evaluate(() => {
    const suspicious = /(?:^|[_-])(access|auth|bearer|jwt|refresh)?token(?:$|[_-])|supadupa.*token/i;
    const entries = [
      ...Object.entries(window.localStorage).map(([key, value]) => ["localStorage", key, value] as const),
      ...Object.entries(window.sessionStorage).map(([key, value]) => ["sessionStorage", key, value] as const),
    ];
    return entries.filter(([, key, value]) => suspicious.test(key) || suspicious.test(value));
  });
  expect(leaked).toEqual([]);
}
