import { expect, test } from "@playwright/test";
import { expectAccessible, logIn } from "./helpers";

test("a signed-out reader is sent to log in, then back where they were", async ({
	page,
}) => {
	await page.goto("/settings");
	await expect(page).toHaveURL("/auth/login?to=%2Fsettings");
	await expect(
		page.getByRole("heading", { name: "Log in to Den" }),
	).toBeVisible();
	await expectAccessible(page);

	await page.getByLabel("Name").fill("Ada");
	await page.getByLabel("Password").fill("wrong");
	await page.getByRole("button", { name: "Log in" }).click();
	await expect(page.getByRole("alert")).toHaveText(
		"That name and password don't match.",
	);

	await page.getByLabel("Password").fill("correct horse");
	await page.getByRole("button", { name: "Log in" }).click();
	await expect(
		page.getByRole("heading", { name: "Who's watching?" }),
	).toBeVisible();
	await expectAccessible(page);
	await page.getByRole("button", { name: "Ada" }).click();
	await expect(page).toHaveURL("/settings");
});

test("the session cookie never reaches the page's script", async ({ page }) => {
	await logIn(page);
	expect(await page.evaluate(() => document.cookie)).not.toContain(
		"photon_session",
	);
	const cookie = (await page.context().cookies()).find(
		(c) => c.name === "photon_session",
	);
	expect(cookie?.httpOnly).toBe(true);
	expect(cookie?.sameSite).toBe("Lax");
});

test("the browser calls the API itself, as its session", async ({ page }) => {
	await logIn(page);
	const me = await page.evaluate(() =>
		fetch("/api/v1/profile").then((r) => r.json()),
	);
	expect(me.name).toBe("Ada");
});

test("logging out from the menu ends the session", async ({ page }) => {
	await logIn(page);
	await page.getByRole("button", { name: "Ada's profile" }).click();
	await page.getByRole("menuitem", { name: "Log out" }).click();
	await expect(page).toHaveURL("/auth/login");
	await page.goto("/");
	await expect(page).toHaveURL("/auth/login");
});
