import { expect, test } from "@playwright/test";
import { expectAccessible, logIn, switchToKids } from "./helpers";

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
	// The server's pages are under Settings, as Plex keeps them: one way in.
	await expect(nav.getByRole("link", { name: "Settings" })).toBeVisible();
	await expect(nav.getByRole("link", { name: "Dashboard" })).toHaveCount(0);
});

test("rows scroll sideways inside the page, which never does", async ({
	page,
}) => {
	await logIn(page);
	for (const [path, row] of [
		["/", "Recently Added in Films"],
		["/titles/t-film", "More like this"],
	]) {
		await page.goto(path);
		await expect(page.getByRole("heading", { name: row })).toBeVisible();
		const { scrolled, viewport } = await page.evaluate(() => ({
			scrolled: document.documentElement.scrollWidth,
			viewport: document.documentElement.clientWidth,
		}));
		expect(scrolled, path).toBe(viewport);
		// And a row scrolls sideways only, drawing no scroll bar: nothing in it
		// reaches below it, no bar takes room under it, and it asks for none
		// (headless Chromium overlays its bars, so the room alone proves little).
		const rows = await page
			.locator("section ul.overflow-x-auto")
			.evaluateAll((uls) =>
				uls.map((ul) => [
					ul.scrollHeight - ul.clientHeight,
					(ul as HTMLElement).offsetHeight - ul.clientHeight,
					getComputedStyle(ul).scrollbarWidth,
				]),
			);
		expect(rows.length, path).toBeGreaterThan(0);
		expect(rows, path).toEqual(rows.map(() => [0, 0, "none"]));
	}
});

test("a row's arrows, above it, page through it, each working only where there is more", async ({
	page,
	isMobile,
}) => {
	test.skip(isMobile, "A touch screen swipes; it has no arrows.");
	await logIn(page);
	const row = page.getByRole("region", { name: /Recently Added in Films/ });
	const previous = row.getByRole("button", { name: /Previous in/ });
	const next = row.getByRole("button", { name: /Next in/ });
	await expect(previous).toBeDisabled();
	await expect(next).toBeEnabled();
	await next.click();
	await expect
		.poll(() => row.locator("ul").evaluate((ul) => ul.scrollLeft))
		.toBeGreaterThan(0);
	await expect(previous).toBeEnabled();
	await expectAccessible(page);
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
	await switchToKids(page);
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

test("a plugin's page opens on its own origin, with a visit's code", async ({
	page,
	isMobile,
}) => {
	await page.route("http://requests.test/**", (route) =>
		route.fulfill({
			contentType: "text/html",
			body: "<title>Requests</title>",
		}),
	);
	await logIn(page);
	if (isMobile)
		await page.getByRole("button", { name: "Toggle Sidebar" }).click();
	const nav = page.getByRole("navigation", { name: "Main" });
	await expect(
		nav.getByRole("button", { name: "Requests waiting" }),
	).toBeVisible();
	await nav.getByRole("button", { name: "Ask for a film" }).click();
	await expect(page).toHaveURL("http://requests.test/ui#photon_visit=c0de");
});
