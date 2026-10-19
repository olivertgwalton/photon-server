import { expect, type Page, test } from "@playwright/test";
import { settings } from "../src/lib/settings.ts";
import { expectAccessible, logIn, switchToKids } from "./helpers";

// Another device saves this change, then the page's stream comes back from a
// drop: the server greets it again, and every page loads afresh.
async function savedElsewhere(page: Page, change: object) {
	await page.request.patch("/api/v1/profile/preferences", { data: change });
	await expect
		.poll(async () => (await page.request.get("/mock/listening")).json())
		.toBeGreaterThan(0);
	await page.request.post("/mock/emit", {
		data: { event: "hello", data: { scans: [] } },
	});
}

test("a setting is found by its name or another word for it, and opened at", async ({
	page,
}) => {
	await logIn(page, "/settings");
	const nav = page.getByRole("navigation", { name: "Settings" });
	const search = nav.getByRole("searchbox", { name: "Search settings" });

	await search.fill("captions");
	await expect(nav.getByRole("link")).toHaveText([
		"Subtitles · Playback",
		"Subtitle language · Playback",
		"Remember the subtitles chosen · Playback",
	]);

	await search.fill("nothing like it");
	await expect(nav.getByText("No settings match.")).toBeVisible();

	await search.fill("remote bitrate");
	await search.press("Enter");
	await expect(page).toHaveURL("/settings/server/network#remote-max");
	await expect(page.getByLabel("Limit (Mbps)")).toBeFocused();
});

test("every setting search finds is on its page", async ({ page }) => {
	await logIn(page, "/settings");
	for (const [href, found] of Object.entries(settings)) {
		await page.goto(href);
		for (const [id] of found) {
			await expect(
				page.locator(`[id="${id}"]`),
				`${href}#${id}`,
			).toBeAttached();
		}
	}
});

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

test("the settings list reaches its last link in a short window", async ({
	page,
}) => {
	await page.setViewportSize({ width: 1280, height: 600 });
	await logIn(page, "/settings/server/general");
	const nav = page.getByRole("navigation", { name: "Settings" });
	const last = nav.getByRole("link", { name: "API keys" });
	await expect(last).not.toBeInViewport();
	// The list scrolls on its own: the page it sits beside stays where it is.
	await last.scrollIntoViewIfNeeded();
	await expect(last).toBeInViewport();
	expect(await page.evaluate(() => window.scrollY)).toBe(0);
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

test("playback settings follow another device's, after one is saved here", async ({
	page,
}) => {
	await logIn(page, "/settings/playback");
	const subtitles = page.getByLabel("Subtitles", { exact: true });
	await subtitles.click();
	await page.getByRole("option", { name: "Always" }).click();
	await expect(page.getByText("Saved for every device.")).toBeVisible();
	await savedElsewhere(page, { subtitle_mode: "none" });
	await expect(subtitles).toHaveText("Never");
});

test("subtitles look as this device was set, in the player", async ({
	page,
}) => {
	await logIn(page, "/settings/playback");
	await page.getByLabel("Subtitle colour").click();
	await page.getByRole("option", { name: "Yellow" }).click();
	await expect(page.getByText("Saved on this device.")).toBeVisible();
	await page.getByLabel("Subtitle size").click();
	await page.getByRole("option", { name: "Larger" }).click();
	await expectAccessible(page);
	await page.reload();
	await expect(page.getByLabel("Subtitle colour")).toHaveText("Yellow");

	await page.goto("/play/p-film?t=1");
	await page.getByRole("button", { name: "Subtitles" }).click();
	const style = await page
		.locator("video")
		.evaluate((v) => getComputedStyle(v).getPropertyValue("--cue-scale"));
	expect(style.trim()).toBe("1.5");
	await expect
		.poll(() =>
			page
				.locator("video")
				.evaluate((v) => getComputedStyle(v).getPropertyValue("--cue-colour")),
		)
		.toBe("#ffe14d");
});

test("a show's episode plays its theme once the profile asks for theme music", async ({
	page,
}) => {
	const tune = page.locator("audio[data-theme-tune]");
	await logIn(page, "/titles/t-ep");
	await expect(page.getByRole("img", { name: "Small Show" })).toBeVisible();
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

test("a profile links its AniList account by the token AniList shows it", async ({
	page,
}) => {
	await logIn(page, "/settings/trackers");
	await page.getByRole("button", { name: "Link AniList", exact: true }).click();
	await expect(
		page.getByRole("link", { name: "Open AniList" }),
	).toHaveAttribute(
		"href",
		"https://anilist.co/api/v2/oauth/authorize?client_id=app&response_type=token",
	);
	await expectAccessible(page);

	await page.getByLabel("AniList token").fill("an.old.token");
	await page.getByRole("button", { name: "Link", exact: true }).click();
	await expect(page.getByText("AniList refused the token")).toBeVisible();

	await page.getByLabel("AniList token").fill("a.year.of.anime");
	await page.getByRole("button", { name: "Link", exact: true }).click();
	await expect(page.getByText("AniList is linked.")).toBeVisible();
	await expect(page.getByText(/Linked as\s+ada/)).toBeVisible();
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
	for (let to = 4; to >= 1; to--) {
		await page.keyboard.press("Enter");
		await expect(
			page.getByText(`Recently Added Films moved to ${to} of 8.`),
		).toBeAttached();
	}
	await expect(rows.first()).toHaveText("Recently Added Films");
	// At the top, focus passes to the arrow that can still move it.
	await expect(
		page.getByRole("button", { name: "Move Recently Added Films down" }),
	).toBeFocused();
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

test("a home row stays where the last of quick moves put it", async ({
	page,
}) => {
	// The first move's save is answered last.
	let answer = () => {};
	const answered = new Promise<void>((done) => {
		answer = done;
	});
	let first = true;
	await page.route("**/api/v1/profile/preferences", async (route) => {
		if (route.request().method() === "PATCH" && first) {
			first = false;
			await answered;
		}
		await route.continue();
	});
	await logIn(page, "/settings/home");
	const rows = page
		.getByRole("list", { name: "Home rows" })
		.getByRole("listitem");
	await page
		.getByRole("button", { name: "Move Recently Added Films up" })
		.focus();
	for (let i = 0; i < 4; i++) await page.keyboard.press("Enter");
	answer();
	await expect(
		page.getByText("Recently Added Films moved to 1 of 8."),
	).toBeAttached();
	await expect(rows.first()).toHaveText("Recently Added Films");
	await expect(
		page.getByRole("button", { name: "Move Recently Added Films down" }),
	).toBeFocused();

	await page.reload();
	await expect(rows.first()).toHaveText("Recently Added Films");
});

test("the home's rows follow another device's order, after one is saved here", async ({
	page,
}) => {
	await logIn(page, "/settings/home");
	const rows = page
		.getByRole("list", { name: "Home rows" })
		.getByRole("listitem");
	await page.getByRole("switch", { name: "Show Continue Watching" }).click();
	await expect(page.getByText("Continue Watching hidden.")).toBeAttached();
	await savedElsewhere(page, {
		home: [
			{ row: "top_rated_unwatched", visibility: "shown" },
			{ row: "continue_watching", visibility: "shown" },
		],
	});
	await expect(rows).toHaveText(["Top Rated", "Continue Watching"]);
	await expect(
		page.getByRole("switch", { name: "Show Continue Watching" }),
	).toHaveAttribute("aria-checked", "true");
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
