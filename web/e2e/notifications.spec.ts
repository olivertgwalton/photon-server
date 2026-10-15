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
	await expect(show).toHaveAttribute("href", "/titles/t-show");
	await expect(show).toContainText("2 new episodes, the latest S1 E2 · Second");
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
	await page.getByRole("button", { name: "Tell me of" }).click();
	await page
		.getByRole("option", { name: "Every new film and episode" })
		.click();
	await expect(page.getByText("Saved for every device.")).toBeVisible();
	await page.reload();
	await expect(page.getByRole("button", { name: "Tell me of" })).toContainText(
		"Every new film and episode",
	);
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

	// Read elsewhere, it is marked here as the server says.
	await page.request.post("/mock/emit", {
		data: {
			event: "notifications.changed",
			data: { change: "read", read: [2], unread: 1 },
		},
	});
	await expect(
		page.getByRole("button", { name: "Notifications: 1 unread" }),
	).toBeVisible();
	expect(asked).toBe(1);
});
