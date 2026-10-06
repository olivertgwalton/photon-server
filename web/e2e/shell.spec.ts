import { expect, test } from "@playwright/test";
import { expectAccessible, logIn } from "./helpers";

test("home draws the rows and the shell lists every library", async ({
	page,
	isMobile,
}) => {
	await logIn(page);
	await expect(
		page.getByRole("heading", { name: "Continue Watching" }),
	).toBeVisible();
	await expect(page.getByRole("link", { name: /Small Show/ })).toHaveAttribute(
		"href",
		"/titles/t-ep",
	);
	await expect(
		page.getByRole("link", { name: /Quiet Hours/ }).first(),
	).toBeVisible();
	await expectAccessible(page);

	if (isMobile)
		await page.getByRole("button", { name: "Toggle Sidebar" }).click();
	const nav = page.getByRole("navigation", { name: "Main" });
	await expect(nav.getByRole("link", { name: "Home" })).toHaveAttribute(
		"aria-current",
		"page",
	);
	await expect(nav.getByRole("link", { name: "Films" })).toHaveAttribute(
		"href",
		"/libraries/l-films",
	);
	await expect(nav.getByRole("link", { name: "Shows" })).toBeVisible();
	await expect(nav.getByRole("link", { name: "Dashboard" })).toBeVisible();
});

test("rows scroll sideways inside the page, which never does", async ({
	page,
}) => {
	await logIn(page);
	for (const [path, row] of [
		["/", "Recently Added Films"],
		["/titles/t-film", "More like this"],
	]) {
		await page.goto(path);
		await expect(page.getByRole("heading", { name: row })).toBeVisible();
		const { scrolled, viewport } = await page.evaluate(() => ({
			scrolled: document.documentElement.scrollWidth,
			viewport: document.documentElement.clientWidth,
		}));
		expect(scrolled, path).toBe(viewport);
	}
});

test("search takes the reader to the results for what they typed", async ({
	page,
}) => {
	await logIn(page);
	await page.getByRole("searchbox", { name: "Search" }).fill("quiet");
	await page.getByRole("searchbox", { name: "Search" }).press("Enter");
	await expect(page).toHaveURL("/search?q=quiet");
});

test("switching profile from the menu asks a locked one for its secret", async ({
	page,
}) => {
	await logIn(page);
	await page.getByRole("button", { name: "Ada's profile" }).click();
	await page.getByRole("menuitem", { name: "Switch profile" }).click();
	await page.getByRole("button", { name: "Kids" }).click();
	await expect(
		page.getByRole("button", { name: "Kids's profile" }),
	).toBeVisible();
	await expect(
		page.getByRole("navigation", { name: "Main" }).getByRole("link", {
			name: "Dashboard",
		}),
	).toHaveCount(0);

	await page.getByRole("button", { name: "Kids's profile" }).click();
	await page.getByRole("menuitem", { name: "Switch profile" }).click();
	await page.getByRole("link", { name: /Ada/ }).click();
	await page.getByLabel("Password").fill("wrong");
	await page.getByRole("button", { name: "Continue" }).click();
	await expect(page.getByRole("alert")).toHaveText(
		"That's not the right PIN or password.",
	);
	await page.getByLabel("Password").fill("correct horse");
	await page.getByRole("button", { name: "Continue" }).click();
	await expect(
		page.getByRole("button", { name: "Ada's profile" }),
	).toBeVisible();
});
