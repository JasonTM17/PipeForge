import AxeBuilder from "@axe-core/playwright";
import { expect, test } from "@playwright/test";

const jobId = "job-0001-aaaa-bbbb";

async function installHappyPathAPI(page: import("@playwright/test").Page) {
  await page.route("**/v1/auth/login", (route) => route.fulfill({ json: { accessToken: "browser-token" } }));
  await page.route("**/v1/datasets?**", (route) => route.fulfill({
    json: { items: [{ id: "dataset-1", name: "Customer orders", state: "READY", updatedAt: "2026-08-03T09:30:00Z" }] },
  }));
  await page.route("**/api/v1/jobs?**", (route) => route.fulfill({
    json: { items: [{ id: jobId, datasetVersionId: "version-1", state: "SUCCEEDED", createdAt: "2026-08-03T09:30:00Z", updatedAt: "2026-08-03T09:31:00Z" }] },
  }));
  await page.route(`**/api/v1/jobs/${jobId}/artifacts?**`, (route) => route.fulfill({
    json: { items: [{ id: "artifact-1", jobId, kind: "CANONICAL_JSON", sizeBytes: 2048, state: "READY" }] },
  }));
}

test("operator can sign in, inspect a job, and use the console accessibly", async ({ page }) => {
  await installHappyPathAPI(page);
  await page.goto("/");

  await expect(page.getByRole("heading", { name: "See the pipeline clearly." })).toBeVisible();
  await page.getByLabel("Email").fill("owner@example.com");
  await page.getByLabel("Password").fill("secret");
  await page.getByRole("button", { name: "Open console" }).click();

  await expect(page.getByRole("heading", { name: "Runtime signal, without the noise." })).toBeVisible();
  await expect(page.getByText("Customer orders")).toBeVisible();
  await page.getByRole("button", { name: /job-0001/i }).click();
  await expect(page.getByText("CANONICAL_JSON")).toBeVisible();
  await expect(page).toHaveURL(new RegExp(`job=${jobId}`));

  const results = await new AxeBuilder({ page }).analyze();
  expect(results.violations).toEqual([]);
});

test("authentication failure is announced without losing form context", async ({ page }) => {
  await page.route("**/v1/auth/login", (route) => route.fulfill({
    status: 401,
    contentType: "application/problem+json",
    body: JSON.stringify({ code: "UNAUTHORIZED", message: "Invalid email or password" }),
  }));
  await page.goto("/");
  await page.getByLabel("Email").fill("owner@example.com");
  await page.getByLabel("Password").fill("wrong");
  await page.getByRole("button", { name: "Open console" }).click();

  await expect(page.getByRole("alert")).toHaveText("Invalid email or password");
  await expect(page.getByLabel("Email")).toHaveValue("owner@example.com");
  await expect(page.getByRole("button", { name: "Open console" })).toBeEnabled();
});
