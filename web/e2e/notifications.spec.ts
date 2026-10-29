import { expect, test } from "@playwright/test";
import { expectAccessible, logIn } from "./helpers";

test("the bell lists what was added that the profile follows, and reads it", async ({
	page,
}) => {
	await logIn(page);
	const bell = page.getByRole("button", { name: "Notifications: 1 unread" });
	await bell.click();
	const inbox = page.getByRole("dialog", { name: "Notifications" });
	const show = inbox.getByRole("link", { name: /Small Show/ });
	await expect(show).toHaveAttribute("href", "/titles/t-show?notification=1");
	await expect(show).toContainText(
		"2 new episodes, the latest S1, E2 · Second",
	);
	await expectAccessible(page);

	await inbox.getByRole("button", { name: "Mark all read" }).click();
	await expect(
		page.getByRole("button", { name: "Notifications", exact: true }),
	).toBeVisible();
	await expect(inbox.getByText("Unread")).toHaveCount(0);
	await inbox.getByRole("link", { name: "Notification settings" }).click();
	await expect(page).toHaveURL("/settings/notifications");
});

test("a profile chooses what it is told of", async ({ page }) => {
	await logIn(page, "/settings/notifications");
	await expect(
		page.getByRole("heading", { name: "Notifications" }),
	).toBeVisible();
	await page.getByRole("combobox", { name: "Tell me of" }).click();
	await page
		.getByRole("option", { name: "Every new film and episode" })
		.click();
	await expect(page.getByText("Saved for every device.")).toBeVisible();
	await page.reload();
	await expect(
		page.getByRole("combobox", { name: "Tell me of" }),
	).toContainText("Every new film and episode");
	// A library cleared tells nothing, on every device.
	await page.getByRole("checkbox", { name: "Films" }).click();
	await expect(page.getByText("Saved for every device.").first()).toBeVisible();
	await page.reload();
	await expect(page.getByRole("checkbox", { name: "Films" })).not.toBeChecked();
	await expect(page.getByRole("checkbox", { name: "Shows" })).toBeChecked();
	// The test's page is plain http, where no browser takes pushes.
	await expect(
		page.getByRole("switch", { name: "Notify this browser" }),
	).toBeDisabled();
	await expectAccessible(page);
});

test("the bell takes what the server tells of, asking it for nothing", async ({
	page,
}) => {
	let asked = 0;
	page.on("request", (r) => {
		if (new URL(r.url()).pathname === "/api/v1/profile/notifications") asked++;
	});
	await logIn(page);
	await expect(
		page.getByRole("button", { name: "Notifications: 1 unread" }),
	).toBeVisible();
	await expect
		.poll(async () => (await page.request.get("/mock/listening")).json())
		.toBeGreaterThan(0);
	await page.request.post("/mock/emit", {
		data: {
			event: "notifications.changed",
			data: {
				change: "added",
				unread: 2,
				notifications: [
					{
						id: 2,
						kind: "title",
						title: {
							id: "t-film",
							kind: "movie",
							title: "Quiet Hours",
							added_at: "2026-10-15T21:00:00Z",
						},
						latest: {
							id: "t-film",
							kind: "movie",
							title: "Quiet Hours",
							added_at: "2026-10-15T21:00:00Z",
						},
						new: 1,
						message: "New film",
						added_at: "2026-10-15T21:00:00Z",
					},
				],
			},
		},
	});
	await page.getByRole("button", { name: "Notifications: 2 unread" }).click();
	const inbox = page.getByRole("dialog", { name: "Notifications" });
	await expect(inbox.getByRole("link").first()).toContainText("Quiet Hours");
	await expect(inbox.getByRole("link", { name: /Small Show/ })).toBeVisible();

	// Opened elsewhere, it leaves here as the server says.
	await page.request.post("/mock/emit", {
		data: {
			event: "notifications.changed",
			data: { change: "cleared", cleared: [2], unread: 1 },
		},
	});
	await expect(inbox.getByRole("link", { name: /Quiet Hours/ })).toHaveCount(0);
	await expect(
		page.getByRole("button", { name: "Notifications: 1 unread" }),
	).toBeVisible();
	expect(asked).toBe(1);
});

test("clearing the bell empties it, here and on the server", async ({
	page,
}) => {
	await logIn(page);
	await page.getByRole("button", { name: "Notifications: 1 unread" }).click();
	const inbox = page.getByRole("dialog", { name: "Notifications" });
	await inbox.getByRole("button", { name: "Clear all" }).click();
	await expect(inbox.getByText("Nothing new")).toBeVisible();
	await expect(inbox.getByRole("button", { name: "Clear all" })).toHaveCount(0);
	await page.reload();
	await page
		.getByRole("button", { name: "Notifications", exact: true })
		.click();
	await expect(
		page
			.getByRole("dialog", { name: "Notifications" })
			.getByText("Nothing new"),
	).toBeVisible();
});

test("opening a notification clears it, here and on the server", async ({
	page,
}) => {
	await logIn(page);
	await page.getByRole("button", { name: "Notifications: 1 unread" }).click();
	await page
		.getByRole("dialog", { name: "Notifications" })
		.getByRole("link", { name: /Small Show/ })
		.click();
	await expect(page).toHaveURL(/\/titles\/t-show$/);
	await page
		.getByRole("button", { name: "Notifications", exact: true })
		.click();
	await expect(
		page
			.getByRole("dialog", { name: "Notifications" })
			.getByText("Nothing new"),
	).toBeVisible();
});

test("a page a push opened clears its notification", async ({ page }) => {
	await logIn(page);
	await page.goto("/titles/t-show?notification=1");
	await expect(page).toHaveURL(/\/titles\/t-show$/);
	await expect(
		page.getByRole("button", { name: "Notifications", exact: true }),
	).toBeVisible();
});

test("the open bell menu stays under the bell as the page scrolls", async ({
	page,
}) => {
	// A short window, so the home page has somewhere to scroll.
	await page.setViewportSize({ width: 1024, height: 400 });
	await logIn(page);
	await page.getByRole("button", { name: "Notifications: 1 unread" }).click();
	await expect(
		page.getByRole("dialog", { name: "Notifications" }),
	).toBeVisible();
	// Measured in the frame the scroll lands in: a menu placed against the
	// document is left behind until it is placed again, which on a real scroll
	// is a menu sliding away from its bell.
	const gaps = await page.evaluate(() =>
		[300, 900, 1800].map((y) => {
			window.scrollTo(0, y);
			const bell = document
				.querySelector('button[aria-label^="Notifications"]')
				?.getBoundingClientRect();
			const menu = document
				.querySelector('[role="dialog"][aria-label="Notifications"]')
				?.getBoundingClientRect();
			return Math.round((menu?.top ?? 0) - (bell?.bottom ?? 0));
		}),
	);
	for (const gap of gaps) expect(Math.abs(gap)).toBeLessThan(16);
});
