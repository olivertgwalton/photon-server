import { expect, test } from "@playwright/test";
import { expectAccessible, logIn, switchToKids } from "./helpers";

test("an admin's settings are one place: their account, then the server", async ({
	page,
}) => {
	await logIn(page, "/settings");
	await expect(page.getByRole("heading", { name: "Ada" })).toBeVisible();
	const nav = page.getByRole("navigation", { name: "Settings" });
	for (const name of [
		"Profile",
		"Playback",
		"Home",
		"Devices",
		"Link a device",
	]) {
		await expect(nav.getByRole("link", { name, exact: true })).toBeVisible();
	}
	await expect(nav.getByRole("link", { name: "Libraries" })).toBeVisible();
	await expect(
		page.getByText("An admin's profile is always opened"),
	).toBeVisible();
	await expectAccessible(page);

	await page.getByLabel("Current password").fill("correct horse");
	await page.getByLabel("New password", { exact: true }).fill("battery staple");
	await page.getByLabel("Repeat the new password").fill("battery stapler");
	await page.getByRole("button", { name: "Change password" }).click();
	await expect(
		page.getByText("The new password and its repeat differ."),
	).toBeVisible();
	await page.getByLabel("Repeat the new password").fill("battery staple");
	await page.getByRole("button", { name: "Change password" }).click();
	await expect(page.getByText(/Password changed/)).toBeVisible();

	await nav.getByRole("link", { name: "Devices" }).click();
	await expect(page).toHaveURL("/settings/devices");
	await expect(page.getByText("This browser")).toBeVisible();
	await expectAccessible(page);
	await page.getByRole("button", { name: "Sign out Living Room" }).click();
	await expect(page.getByText("That device is signed out.")).toBeVisible();
});

test("the dashboard's old addresses lead to its new ones", async ({ page }) => {
	await logIn(page, "/admin/libraries");
	await expect(page).toHaveURL("/settings/server/libraries");
	await expect(page.getByRole("heading", { name: "Libraries" })).toBeVisible();
	await page.goto("/admin");
	await expect(page).toHaveURL("/settings/server");
});

test("a member sets a PIN and takes it off again, and sees no server", async ({
	page,
}) => {
	await logIn(page);
	await page.goto("/profiles?to=%2Fsettings");
	await switchToKids(page);
	await expect(page).toHaveURL("/settings");
	await expect(
		page.getByRole("navigation", { name: "Settings" }).getByRole("link", {
			name: "Libraries",
		}),
	).toHaveCount(0);

	await page.getByLabel("PIN", { exact: true }).fill("2468");
	await page.getByRole("button", { name: "Set PIN" }).click();
	await expect(page.getByText("PIN set.")).toBeVisible();
	await expect(
		page.getByText("Switching to this profile asks for its PIN."),
	).toBeVisible();

	await page.getByRole("button", { name: "Remove PIN" }).click();
	await expect(page.getByText("PIN removed.")).toBeVisible();
	await expect(page.getByRole("button", { name: "Set PIN" })).toBeVisible();
});

test("playback settings are the profile's, kept by the server", async ({
	page,
}) => {
	await logIn(page, "/settings/playback");
	await expectAccessible(page);
	await page.getByLabel("Subtitles", { exact: true }).click();
	await page.getByRole("option", { name: "Always" }).click();
	await expect(page.getByText("Saved for every device.")).toBeVisible();
	await page.getByRole("switch", { name: "Play the next episode" }).click();
	await page.reload();
	await expect(page.getByLabel("Subtitles", { exact: true })).toHaveText(
		"Always",
	);
	await expect(
		page.getByRole("switch", { name: "Play the next episode" }),
	).toHaveAttribute("aria-checked", "false");
});

test("a show's episode plays its theme once the profile asks for theme music", async ({
	page,
}) => {
	const tune = page.locator("audio[data-theme-tune]");
	await logIn(page, "/titles/t-ep");
	await expect(page.getByRole("heading", { name: "Pilot" })).toBeVisible();
	await expect(tune).toHaveCount(0);
	await page.goto("/settings/playback");
	await page.getByRole("switch", { name: "Play theme music" }).click();
	await expect(page.getByText("Saved for every device.")).toBeVisible();
	await page.goto("/titles/t-ep");
	await expect(tune).toHaveAttribute("src", "/api/v1/themes/th-small-show");
});

test("a TV is linked by the code it shows", async ({ page }) => {
	await logIn(page, "/settings/link");
	await expectAccessible(page);
	const code = page.getByLabel("Code");
	await code.fill("ZZZZ-ZZZZ");
	await page.getByRole("button", { name: "Link" }).click();
	await expect(page.getByRole("alert")).toHaveText(
		/No device is showing that code/,
	);

	await code.fill("bcdf-ghjk");
	await page.getByRole("button", { name: "Link" }).click();
	await expect(page.getByRole("status")).toHaveText(
		"Living Room (Photon for tvOS) is signed in.",
	);
});

test("a profile links its Trakt account by the code Trakt gives it", async ({
	page,
}) => {
	await logIn(page, "/settings/trackers");
	await expect(page.getByText(/Simkl\s+is not set up yet/)).toBeVisible();
	await expectAccessible(page);
	await expect
		.poll(async () => (await page.request.get("/mock/listening")).json())
		.toBeGreaterThan(0);

	await page.getByRole("button", { name: "Link Trakt", exact: true }).click();
	await expect(page.getByText("TRAKT123")).toBeVisible();
	await expect(page.getByText(/Expires in \d+:\d\d/)).toBeVisible();
	await expect(page.getByRole("link", { name: "Open Trakt" })).toHaveAttribute(
		"href",
		"https://trakt.tv/activate/TRAKT123",
	);
	await expectAccessible(page);

	// Trakt is entered on another device, and the server tells the page.
	await page.request.post("/mock/tracker-entered");
	await expect(page.getByText(/Linked as\s+ada/)).toBeVisible();

	await page.getByRole("button", { name: "Unlink Trakt" }).click();
	await page
		.getByRole("alertdialog")
		.getByRole("button", { name: "Unlink" })
		.click();
	await expect(page.getByText("Trakt is unlinked.")).toBeVisible();
	await expect(
		page.getByRole("button", { name: "Link Trakt", exact: true }),
	).toBeVisible();
});

test("an admin sets up a tracker by its app's client id", async ({ page }) => {
	await logIn(page, "/settings/server/trackers");
	await expectAccessible(page);
	await page.getByLabel("Client id").nth(1).fill(" simkl-app ");
	await page.getByRole("button", { name: "Save Simkl" }).click();
	await expect(page.getByText("Simkl was saved.")).toBeVisible();
	await expect(page.getByLabel("Client id").nth(1)).toHaveValue("simkl-app");
});

test("the address a TV shows fills in its code", async ({ page }) => {
	await logIn(page, "/link?code=BCDF-GHJK");
	await expect(page).toHaveURL("/settings/link?code=BCDF-GHJK");
	await expect(page.getByLabel("Code")).toHaveValue("BCDF-GHJK");
	await page.getByRole("button", { name: "Link" }).click();
	await expect(page.getByRole("status")).toHaveText(
		"Living Room (Photon for tvOS) is signed in.",
	);
});

test("the home's rows are put in order and hidden, by pointer or keyboard", async ({
	page,
}) => {
	await logIn(page, "/settings/home");
	await expectAccessible(page);
	const rows = page
		.getByRole("list", { name: "Home rows" })
		.getByRole("listitem");
	await expect(rows).toHaveText([
		"Continue Watching",
		"Next Up",
		"Watchlist",
		"Favourites",
		"Recently Added Films",
		"Recently Added Shows",
		"Recently Released",
		"Top Rated",
	]);

	// The arrow keeps focus as its row climbs, so Enter climbs again.
	await page
		.getByRole("button", { name: "Move Recently Added Films up" })
		.focus();
	for (let i = 0; i < 4; i++) await page.keyboard.press("Enter");
	await expect(rows.first()).toHaveText("Recently Added Films");
	await expect(
		page.getByText("Recently Added Films moved to 1 of 8."),
	).toBeAttached();
	await page.getByRole("switch", { name: "Show Continue Watching" }).click();

	await page.reload();
	await expect(rows.first()).toHaveText("Recently Added Films");
	await expect(
		page.getByRole("switch", { name: "Show Continue Watching" }),
	).toHaveAttribute("aria-checked", "false");

	await page.getByRole("link", { name: "Home", exact: true }).first().click();
	await expect(
		page.getByRole("heading", { name: "Recently Added in Films" }),
	).toBeVisible();
	await expect(
		page.getByRole("heading", { name: "Continue Watching" }),
	).toHaveCount(0);
});

test("a profile is given a picture, shown wherever it is", async ({ page }) => {
	await logIn(page, "/settings");
	const menu = page.getByRole("button", { name: "Ada's profile" });
	await expect(menu.getByText("A", { exact: true })).toBeVisible();
	await expectAccessible(page);

	await page.getByLabel("Picture for Ada").setInputFiles({
		name: "notes.txt",
		mimeType: "text/plain",
		buffer: Buffer.from("not a picture"),
	});
	await expect(
		page.getByText(/a JPEG, PNG, GIF or WebP is kept/),
	).toBeVisible();

	await page.getByLabel("Picture for Ada").setInputFiles({
		name: "ada.png",
		mimeType: "image/png",
		buffer: Buffer.from(
			"iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAYAAAAfFcSJAAAADUlEQVR42mNkqGcAAAIEAQDeEycgAAAAAElFTkSuQmCC",
			"base64",
		),
	});
	await expect(page.getByText("Ada's picture was saved.")).toBeVisible();
	await expect(menu.locator("img")).toHaveAttribute(
		"src",
		/\/api\/v1\/artwork\//,
	);

	await page.getByRole("button", { name: "Remove picture" }).click();
	await expect(page.getByText("Ada's picture was removed.")).toBeVisible();
	// Its initial shows again.
	await expect(menu.getByText("A", { exact: true })).toBeVisible();
});

test("a profile renames itself, to a name no other has", async ({ page }) => {
	await logIn(page, "/settings");
	const name = page.getByLabel("Name", { exact: true });
	await name.fill("Kids");
	await page.getByRole("button", { name: "Rename" }).click();
	await expect(page.getByText(/already exists/)).toBeVisible();

	await name.fill("  Ada Lovelace ");
	await page.getByRole("button", { name: "Rename" }).click();
	await expect(page.getByText("Renamed, on every device.")).toBeVisible();
	await expect(
		page.getByRole("heading", { name: "Ada Lovelace" }),
	).toBeVisible();
	await expect(
		page.getByRole("button", { name: "Ada Lovelace's profile" }),
	).toBeVisible();

	await name.fill("Ada");
	await page.getByRole("button", { name: "Rename" }).click();
	await expect(
		page.getByRole("heading", { name: "Ada", exact: true }),
	).toBeVisible();
});
