import { expect, type Page, test } from "@playwright/test";
import { expectAccessible, logIn, switchToKids } from "./helpers";

// Dialogs open without animating, so a check never reads one half faded in.
test.use({ reducedMotion: "reduce" });

async function asKids(page: Page) {
	await logIn(page);
	await page.goto("/profiles");
	await switchToKids(page);
	await expect(
		page.getByRole("button", { name: "Kids's profile" }),
	).toBeVisible();
}

test("the overview follows the server live: who is playing, and a scan as it grows", async ({
	page,
}) => {
	await logIn(page, "/settings/server");
	await expect(page.getByRole("heading", { name: "Den" })).toBeVisible();
	// What wants looking at comes first, with where to put it right.
	const attention = page.getByRole("region", { name: "Needs attention" });
	await expect(attention).toContainText(
		"1 job failed every attempt and waits to be tried again.",
	);
	await expect(
		attention.getByRole("link", { name: "See jobs" }),
	).toHaveAttribute("href", "/settings/server/jobs");

	const card = page.locator("article", { hasText: "Quiet Hours" });
	await expect(card).toContainText("Kids · Living Room · Photon for tvOS");
	await expect(card).toContainText("Transcode");
	await expect(card).toContainText("Hardware · VideoToolbox");
	await expect(card).toContainText("Because of: Video codec");
	await expect(card).toContainText(
		"hevc 3840×2160 HDR10 → h264 1920×1080 tone mapped",
	);
	await expect(page.getByText("1 of 2 transcoding")).toBeVisible();

	// The snapshot's scan, then the walk finding more folders.
	const scan = page.getByRole("progressbar", { name: "Scanning Films" });
	await expect(scan).toBeVisible();
	await expect(page.getByText("Reading folders: 3 of 10")).toBeVisible();
	await expect(page.getByText(/Reading folders: \d+ of 40/)).toBeVisible();
	await expect(
		page.getByText("Back up the database failed: disk full"),
	).toBeVisible();
	await expectAccessible(page);

	await card.getByRole("button", { name: "Stop this play" }).click();
	await page
		.getByRole("alertdialog")
		.getByRole("button", { name: "Stop" })
		.click();
	await expect(page.getByText("Kids's play was stopped.")).toBeVisible();
});

test("an admin sees what each node does and how busy it is, and sets what one does", async ({
	page,
}) => {
	await logIn(page, "/settings/server");
	const nodes = page.getByRole("table", { name: "Nodes" });
	const gpu = nodes.getByRole("row", { name: /gpu-1/ });
	await expect(gpu).toContainText("Serves and transcodes");
	await expect(gpu).toContainText("NVENC · H.264, HEVC");
	await expect(gpu).toContainText("3 of 8");
	await expect(gpu).toContainText("1 for downloads");
	await expect(
		nodes.getByRole("row", { name: /den \(this node\)/ }),
	).toContainText("VA-API · H.264, HEVC · subtitles");
	const old = nodes.getByRole("row", { name: /old-mini/ });
	await expect(old).toContainText("Not running");
	await expect(old).toContainText("Serves only");
	await expectAccessible(page);

	await gpu.getByRole("button", { name: "Actions for gpu-1" }).click();
	await page.getByRole("menuitem", { name: "Settings…" }).click();
	const dialog = page.getByRole("dialog", { name: "gpu-1" });
	await dialog.getByLabel("Role").click();
	await page.getByRole("option", { name: "Transcodes first" }).click();
	await expect(dialog).toContainText("the server with the GPU");
	await expect(dialog.getByLabel("Transcodes at once")).toHaveText(
		"Worked out from its encoder (8)",
	);
	await dialog.getByLabel("Transcodes at once").click();
	await page.getByRole("option", { name: "At most" }).click();
	await dialog.getByLabel("Most transcodes at once").fill("12");
	// Where the others reach it, which it takes up at once.
	await expect(dialog.getByLabel("Address")).toHaveValue(
		"http://10.0.0.5:8640",
	);
	await dialog.getByLabel("Address").fill("http://10.0.0.6:8640");
	await expectAccessible(page);
	const saved = page.waitForRequest(
		(r) => r.method() === "PATCH" && r.url().endsWith("/admin/nodes/n-2"),
	);
	await dialog.getByRole("button", { name: "Save" }).click();
	expect((await saved).postDataJSON()).toMatchObject({
		role: "transcode",
		address: "http://10.0.0.6:8640",
		transcode_limit: 12,
	});
	await expect(
		page.getByText("Saved. gpu-1 takes it up at once."),
	).toBeVisible();
	await expect(gpu).toContainText("Transcodes first");

	// Drained before its driver is updated: its streams finish, and nothing new is given it.
	await gpu.getByRole("button", { name: "Actions for gpu-1" }).click();
	await page.getByRole("menuitem", { name: "Drain…" }).click();
	const drain = page.getByRole("alertdialog", { name: "Drain gpu-1?" });
	await expect(drain).toContainText("It will finish its 3 current streams");
	await expect(drain).toContainText("Other nodes take new streams meanwhile.");
	await drain.getByLabel("Note for other admins").fill("Driver update");
	await expectAccessible(page);
	await drain.getByRole("button", { name: "Drain gpu-1" }).click();
	await expect(page.getByText("gpu-1 is draining.")).toBeVisible();
	await expect(gpu).toContainText("Draining · 3 streams left");
	await expect(gpu).toContainText("“Driver update”");

	await gpu.getByRole("button", { name: "Actions for gpu-1" }).click();
	await page.getByRole("menuitem", { name: "Resume" }).click();
	await expect(page.getByText("gpu-1 takes new streams again.")).toBeVisible();
	await expect(gpu).not.toContainText("Draining");

	// Taken away for good, it is forgotten; one running is not offered to be.
	await gpu.getByRole("button", { name: "Actions for gpu-1" }).click();
	await expect(page.getByRole("menuitem", { name: "Forget…" })).toHaveCount(0);
	await page.keyboard.press("Escape");
	await old.getByRole("button", { name: "Actions for old-mini" }).click();
	await page.getByRole("menuitem", { name: "Forget…" }).click();
	const forget = page.getByRole("alertdialog", { name: "Forget old-mini?" });
	await expect(forget).toContainText("It was last seen");
	await expectAccessible(page);
	await forget.getByRole("button", { name: "Forget old-mini" }).click();
	await expect(page.getByText("old-mini is forgotten.")).toBeVisible();
	await expect(old).toHaveCount(0);
});

test("a member is told the dashboard is not theirs", async ({ page }) => {
	await asKids(page);
	await page.goto("/settings/server");
	await expect(
		page.getByRole("heading", { name: "only an admin may" }),
	).toBeVisible();
	await expect(page.getByText("403")).toBeVisible();
});

test("a library is added from a folder found by browsing the server", async ({
	page,
}) => {
	await logIn(page, "/settings/server/libraries");
	await expect(page.getByRole("heading", { name: "Films" })).toBeVisible();
	await expect(page.getByText("250 films")).toBeVisible();
	await expect(page.getByText(/Reading folders/)).toBeVisible();
	await expectAccessible(page);

	await page.getByRole("link", { name: "Add a library" }).click();
	await page.getByRole("textbox", { name: "Name" }).fill("Documentaries");
	await page.getByRole("button", { name: "Browse…" }).click();
	const browser = page.getByRole("dialog", { name: "Choose a folder" });
	await browser.getByRole("button", { name: "media" }).click();
	await browser.getByRole("button", { name: "films" }).click();
	await expect(browser.getByText("No folders in here.")).toBeVisible();
	await browser.getByRole("button", { name: "Choose this folder" }).click();
	await expect(page.getByRole("textbox", { name: "Folder" })).toHaveValue(
		"/media/films",
	);
	// A shows library ranks its sources for shows, seasons and episodes, under its settings a
	// new library leaves to their defaults.
	await page.getByRole("button", { name: "Holds" }).click();
	await page.getByRole("option", { name: "Shows" }).click();
	await page.getByText("More settings").click();
	for (const items of ["Shows", "Seasons", "Episodes"]) {
		await expect(
			page.getByRole("list", { name: `Metadata downloaders (${items})` }),
		).toBeVisible();
		await expect(
			page.getByRole("list", { name: `Image fetchers (${items})` }),
		).toBeVisible();
	}
	await expect(
		page
			.getByRole("list", { name: "Image fetchers (Episodes)" })
			.getByRole("checkbox", { name: "TheTVDB" }),
	).not.toBeChecked();
	await expectAccessible(page);
	await page.getByRole("button", { name: "Holds" }).click();
	await page.getByRole("option", { name: "Films" }).click();
	await page.getByRole("button", { name: "Add and scan" }).click();
	await expect(page).toHaveURL("/settings/server/libraries");

	await page.getByRole("link", { name: "Edit Films" }).click();
	// The library's own page does what the list does.
	await expect(
		page.getByRole("button", { name: /Scan now|Scanning/ }),
	).toBeVisible();
	await expect(
		page.getByRole("button", { name: "Refresh metadata of Films" }),
	).toBeVisible();
	await expect(page.getByRole("button", { name: "Remove" })).toBeVisible();
	await expect(page.getByRole("textbox", { name: "Folder" })).toHaveValue(
		"/media/films",
	);
	await expectAccessible(page);

	// MDBList is ticked and trusted over TMDB; the NFO keeps its place unticked.
	const metadata = page.getByRole("list", {
		name: "Metadata downloaders (Films)",
	});
	await expect(metadata.getByRole("listitem")).toHaveText([
		/Nfo/,
		/TMDB/,
		/MDBList/,
	]);
	await metadata.getByRole("checkbox", { name: "Nfo" }).click();
	await metadata.getByRole("checkbox", { name: "MDBList" }).click();
	await metadata.getByRole("button", { name: "Trust MDBList more" }).click();
	await page.getByRole("checkbox", { name: "German", exact: true }).click();
	const saved = page.waitForRequest(
		(r) =>
			r.method() === "PATCH" &&
			r.url().endsWith("/api/v1/admin/libraries/l-films"),
	);
	await page.getByRole("button", { name: "Save" }).click();
	const body = (await saved).postDataJSON();
	expect(body.subtitle_languages).toEqual(["fr", "de"]);
	expect(body.sources).toEqual([
		{
			kind: "movie",
			metadata: [
				{ source: "nfo", enabled: false },
				{ source: "mdblist", enabled: true },
				{ source: "tmdb", enabled: true },
			],
		},
	]);
	await expect(page.getByText("Saved.")).toBeVisible();
});

test("a library's metadata is refreshed, what is missing or all of it", async ({
	page,
}) => {
	await logIn(page, "/settings/server/libraries");
	await page.getByRole("button", { name: "Refresh metadata of Films" }).click();
	const dialog = page.getByRole("dialog", { name: "Refresh metadata" });
	await expect(dialog.getByText("Choose how much of Films")).toBeVisible();
	await expectAccessible(page);

	const asked = page.waitForRequest(
		"**/api/v1/admin/libraries/l-films/refresh",
	);
	await dialog
		.getByRole("button", { name: /Refresh missing metadata/ })
		.click();
	expect((await asked).postDataJSON()).toEqual({ mode: "missing" });
	await expect(
		page.getByText("Fetching missing metadata for Films."),
	).toBeVisible();
	await expect(dialog).toBeHidden();

	await page.getByRole("button", { name: "Refresh metadata of Films" }).click();
	await dialog.getByRole("button", { name: "Cancel" }).click();
	await expect(dialog).toBeHidden();
});

test("a library is scanned, refreshed and opened from its menu in the sidebar", async ({
	page,
}) => {
	await logIn(page);
	const nav = page.getByRole("navigation", { name: "Main" });
	const menu = nav.getByRole("button", { name: "More for Films" });

	const scanned = page.waitForRequest("**/api/v1/admin/libraries/l-films/scan");
	await menu.click();
	await page.getByRole("menuitem", { name: "Scan library files" }).click();
	await scanned;
	await expect(page.getByText("Films is being scanned.")).toBeVisible();

	const asked = page.waitForRequest(
		"**/api/v1/admin/libraries/l-films/refresh",
	);
	await menu.click();
	await page.getByRole("menuitem", { name: "Refresh metadata…" }).click();
	await page
		.getByRole("dialog", { name: "Refresh metadata" })
		.getByRole("button", { name: /Refresh all metadata/ })
		.click();
	expect((await asked).postDataJSON()).toEqual({ mode: "all" });

	// Removing asks first.
	await menu.click();
	await page.getByRole("menuitem", { name: "Remove…" }).click();
	await page
		.getByRole("alertdialog")
		.getByRole("button", { name: "Cancel" })
		.click();

	await menu.click();
	await page.getByRole("menuitem", { name: "Edit…" }).click();
	await expect(page).toHaveURL("/settings/server/libraries/l-films");

	// Removed from its own settings, it leads back to the libraries.
	await page.getByRole("button", { name: "Remove Films" }).click();
	await page
		.getByRole("alertdialog")
		.getByRole("button", { name: "Remove library" })
		.click();
	await expect(page).toHaveURL("/settings/server/libraries");
});

test("a member puts the libraries in their own order, and may do no more", async ({
	page,
}) => {
	await asKids(page);
	const nav = page.getByRole("navigation", { name: "Main" });
	await nav.getByRole("button", { name: "More for Films" }).click();
	await expect(page.getByRole("menuitem", { name: "Edit…" })).toHaveCount(0);
	await page.getByRole("menuitem", { name: "Reorder" }).click();

	// Films is dragged by its grip below Shows.
	const names = nav.getByRole("link", { name: /^(Films|Shows)$/ });
	const grip = nav.getByRole("button", { name: "Drag Films into place" });
	const shows = await nav.getByRole("link", { name: "Shows" }).boundingBox();
	if (!shows) throw new Error("Shows is not drawn");
	const asked = page.waitForRequest("**/api/v1/profile/library-order");
	await grip.hover();
	await page.mouse.down();
	await page.mouse.move(shows.x + 10, shows.y + shows.height, { steps: 5 });
	await page.mouse.up();
	expect((await asked).postDataJSON()).toEqual({
		library_ids: ["l-shows", "l-films"],
	});
	await expect(names).toHaveText(["Shows", "Films"]);

	// And moved back up, and down again, by the keyboard.
	const back = page.waitForRequest("**/api/v1/profile/library-order");
	await grip.focus();
	await page.keyboard.press("ArrowUp");
	expect((await back).postDataJSON()).toEqual({
		library_ids: ["l-films", "l-shows"],
	});
	await expect(names).toHaveText(["Films", "Shows"]);
	await page.keyboard.press("ArrowDown");
	await expect(names).toHaveText(["Shows", "Films"]);

	await nav.getByRole("button", { name: "Done" }).click();
	await expect(grip).toHaveCount(0);
	// The order is the profile's, kept by the server.
	await page.reload();
	await expect(names).toHaveText(["Shows", "Films"]);
});

test("a profile is added seeing one library, and what another may see is set", async ({
	page,
}) => {
	await logIn(page, "/settings/profiles");
	await expectAccessible(page);
	await page.getByRole("button", { name: "Add a profile" }).click();
	await page.getByRole("textbox", { name: "Name" }).fill("Guest");
	await page.getByLabel("Password").fill("battery staple");
	await page.getByLabel("Every library, including ones added later").click();
	await page.getByRole("checkbox", { name: "Films" }).check();
	await expectAccessible(page);
	const limited = page.waitForRequest("**/api/v1/admin/profiles/p-new/access");
	await page.getByRole("button", { name: "Add profile" }).click();
	expect((await limited).postDataJSON()).toEqual({
		max_age: null,
		unrated: "allow",
		libraries: ["l-films"],
	});
	await expect(page.getByText("Guest was added.")).toBeVisible();

	await page.getByRole("link", { name: "Kids" }).click();
	await expect(
		page.getByLabel("Every library, including ones added later"),
	).toBeChecked();
	await expect(page.getByRole("button", { name: "Certificates" })).toHaveText(
		"Up to 12",
	);
	await expectAccessible(page);
	await page.getByRole("button", { name: "Save access" }).click();
	await expect(
		page.getByText("What this profile sees was saved."),
	).toBeVisible();
});

test("a provider is given its key, which is never shown back", async ({
	page,
}) => {
	await logIn(page, "/settings/server/providers");
	await expect(page.getByText("Needs settings").first()).toBeVisible();
	await expectAccessible(page);
	await page.getByLabel("API key (required)").fill("abc123");
	await page.getByRole("button", { name: "Save MDBList settings" }).click();
	await expect(page.getByText("Saved.")).toBeVisible();
});

test("tasks run, dead jobs are retried, and the logs read", async ({
	page,
}) => {
	await logIn(page, "/settings/server/tasks");
	await expect(page.getByText("Failed: disk full")).toBeVisible();
	await expectAccessible(page);
	await page
		.getByRole("button", { name: "Run now Back up the database" })
		.click();
	await expect(
		page.getByText("Back up the database is running."),
	).toBeVisible();

	// Stopped, the question closes, though the task it was asked of stays.
	await page.getByRole("button", { name: "Stop Make previews" }).click();
	await page
		.getByRole("alertdialog")
		.getByRole("button", { name: "Stop" })
		.click();
	await expect(page.getByText("Make previews was stopped.")).toBeVisible();
	await expect(page.getByRole("alertdialog")).toHaveCount(0);

	// The window runs past midnight, kept as the page loads again.
	await page.getByLabel("Until").click();
	await page.getByRole("option", { name: "06:00" }).click();
	await page.getByLabel("From").click();
	await page.getByRole("option", { name: "23:00" }).click();
	await page.getByRole("button", { name: "Save" }).click();
	await expect(page.getByText("Saved.")).toBeVisible();
	await page.reload();
	await expect(page.getByLabel("From")).toHaveText("23:00");
	await expect(page.getByLabel("Until")).toHaveText("06:00");

	await page.goto("/settings/server/jobs");
	await expect(page.getByText("TMDB said 503")).toBeVisible();
	await expectAccessible(page);
	await page.getByRole("button", { name: "Try again job 41" }).click();
	await expect(page.getByText("The job is queued again.")).toBeVisible();
	await expect(page.getByText("Nothing has been given up on.")).toBeVisible();

	await page.goto("/settings/server/activity");
	// The choice applies as it is made.
	await page.getByLabel("Show", { exact: true }).click();
	await page.getByRole("option", { name: "Libraries added" }).click();
	await expect(page).toHaveURL("/settings/server/activity?kind=library.added");
	await expect(page.getByText("Library Films was added")).toBeVisible();
	await expect(page.getByText("Back up the database failed")).toHaveCount(0);
	await expectAccessible(page);

	await page.goto("/settings/server/history");
	await expect(page.getByRole("cell", { name: "Kids" })).toBeVisible();
	await expect(page.getByRole("cell", { name: "100%" })).toBeVisible();
	await expectAccessible(page);
});

test("a webhook's secret is shown once, to copy", async ({ page }) => {
	await logIn(page, "/settings/server/webhooks");
	await page.getByRole("button", { name: "Add a webhook" }).click();
	await page.getByLabel("Address").fill("https://example.com/hook");
	await page.getByLabel("A play starts").click();
	await expectAccessible(page);
	await page.getByRole("button", { name: "Add webhook" }).click();
	const secret = page.getByRole("dialog", { name: "The webhook's secret" });
	await expect(secret.getByLabel("Secret")).toHaveValue("S3CRET-ONCE");
	await expectAccessible(page);
	await secret.getByRole("button", { name: "Close" }).click();

	await expect(page.getByText("A play starts")).toBeVisible();
	await page.getByRole("button", { name: /Send a test/ }).click();
	await expect(page.getByText(/A test was sent/)).toBeVisible();
	await page.getByRole("button", { name: /^Remove/ }).click();
	await page.getByRole("button", { name: "Remove webhook" }).click();
	await expect(page.getByText("No webhooks yet.")).toBeVisible();
});

test("an API key is shown once, to copy, and revoked", async ({ page }) => {
	await logIn(page, "/settings/server/keys");
	await page.getByRole("button", { name: "Make a key" }).click();
	await page.getByLabel("What it is for").fill("Sonarr");
	await expectAccessible(page);
	await page.getByRole("button", { name: "Make key" }).click();
	const made = page.getByRole("dialog", { name: "The new API key" });
	await expect(made.getByLabel("Key")).toHaveValue("pst_ONCE");
	await expectAccessible(page);
	await made.getByRole("button", { name: "Close" }).click();

	await expect(page.getByText("Made by Oliver")).toBeVisible();
	await page.getByRole("button", { name: /^Revoke/ }).click();
	await page.getByRole("button", { name: "Revoke key" }).click();
	await expect(page.getByText("No API keys yet.")).toBeVisible();
});

test("artwork is moved to a bucket that is checked first, and the move may be cancelled", async ({
	page,
}) => {
	await logIn(page, "/settings/server/storage");
	await expect(page.getByLabel("Kept on")).toHaveText("Each server's own disk");
	await page.getByLabel("Kept on").click();
	await page.getByRole("option", { name: "An S3-compatible bucket" }).click();
	await page
		.getByLabel("Address", { exact: true })
		.fill("https://s3.example.com");
	await page.getByLabel("Name").fill("missing");
	await page.getByRole("button", { name: "Check" }).click();
	await expect(page.getByText('there is no bucket "missing"')).toBeVisible();
	await page.getByLabel("Name").fill("photon");
	await page.getByLabel("Access key").fill("AKIA");
	await page.getByLabel("Secret key").fill("secret");
	await page.getByRole("button", { name: "Check" }).click();
	await expect(page.getByText(/^The bucket answers/)).toBeVisible();
	await expectAccessible(page);

	await page.getByRole("button", { name: "Save" }).click();
	await expect(page.getByText(/^Moving what is kept/)).toBeVisible();
	await expect(
		page.getByRole("heading", { name: "Moving to the bucket photon" }),
	).toBeVisible();
	await expect(page.getByText("120 of 400")).toBeVisible();
	await expect(
		page.getByRole("progressbar", { name: "Copying This server's disk" }),
	).toBeVisible();
	await expect(page.getByText("Listing what to copy")).toBeVisible();
	await expectAccessible(page);

	await page.getByRole("button", { name: "Cancel move" }).click();
	await page
		.getByRole("alertdialog")
		.getByRole("button", { name: "Cancel move" })
		.click();
	await expect(page.getByText(/^Cancelled/)).toBeVisible();
	await expect(page.getByLabel("Kept on")).toHaveText("Each server's own disk");
});

test("secure connections are set with a certificate", async ({ page }) => {
	await logIn(page, "/settings/server/network");
	await expectAccessible(page);
	await page.getByLabel("Secure connections").click();
	await page.getByRole("option", { name: "Preferred" }).click();
	await page.getByLabel("Certificate").fill("/certs/fullchain.pem");
	await page.getByLabel("Key").fill("/certs/privkey.pem");
	await page.getByRole("button", { name: "Save" }).click();
	await expect(page.getByText(/^Saved/)).toBeVisible();

	await page.reload();
	await expect(page.getByLabel("Secure connections")).toHaveText("Preferred");
	await expect(page.getByLabel("Certificate")).toHaveValue(
		"/certs/fullchain.pem",
	);
});

test("Jellyfin's apps are let in on a port of their own", async ({ page }) => {
	await logIn(page, "/settings/server/network");
	await page.getByLabel("Jellyfin apps").click();
	await page.getByRole("option", { name: "On" }).click();
	await page.getByLabel("Port").fill("8097");
	await page.getByRole("button", { name: "Save" }).click();
	await expect(page.getByText(/^Saved/)).toBeVisible();

	await page.reload();
	await expect(page.getByLabel("Jellyfin apps")).toHaveText("On");
	await expect(page.getByLabel("Port")).toHaveValue("8097");
	await expectAccessible(page);
});

test("remote streams are kept within a limit, in Mbps", async ({ page }) => {
	await logIn(page, "/settings/server/network");
	await page.getByLabel("Local networks").fill("192.168.1.0/24, 100.64.0.0/10");
	await page.getByLabel("Limit (Mbps)").fill("8");
	const saved = page.waitForRequest(
		(r) => r.method() === "PUT" && r.url().endsWith("/api/v1/admin/network"),
	);
	await page.getByRole("button", { name: "Save" }).click();
	const body = (await saved).postDataJSON();
	expect(body.remote_max_bitrate_kbps).toBe(8000);
	expect(body.local_networks).toEqual(["192.168.1.0/24", "100.64.0.0/10"]);
	await expect(page.getByText(/^Saved/)).toBeVisible();

	await page.reload();
	await expect(page.getByLabel("Limit (Mbps)")).toHaveValue("8");
	await expect(page.getByLabel("Local networks")).toHaveValue(
		"192.168.1.0/24, 100.64.0.0/10",
	);
	await expectAccessible(page);
});

test("an admin names the server and sets what it describes titles in", async ({
	page,
}) => {
	await logIn(page, "/settings/server/general");
	await expect(page.getByLabel("Name")).toHaveValue("Den");
	await expect(page.getByLabel("Metadata language")).toHaveText(
		"British English",
	);
	await page.getByLabel("Name").fill("Lounge");
	await page.getByLabel("Metadata language").click();
	await page.getByRole("option", { name: "German (Germany)" }).click();
	await page.getByLabel("Certification country").click();
	await page.getByRole("option", { name: "Germany" }).click();
	await expectAccessible(page);
	const saved = page.waitForRequest(
		(r) => r.method() === "PUT" && r.url().endsWith("/api/v1/admin/server"),
	);
	await page.getByRole("button", { name: "Save" }).click();
	expect((await saved).postDataJSON()).toEqual({
		name: "Lounge",
		metadata_language: "de-DE",
		certification_country: "DE",
	});
	await expect(page.getByText(/^Saved/)).toBeVisible();
	await expect(page.getByRole("heading", { name: "General" })).toBeVisible();
	await page.goto("/settings/server");
	await expect(page.getByRole("heading", { name: "Lounge" })).toBeVisible();
	// The mock is every test's server: it is put back as it was.
	await page.request.put("/api/v1/admin/server", {
		data: {
			name: "Den",
			metadata_language: "en-GB",
			certification_country: "GB",
		},
	});
});

test("an admin sets how the server is reached from outside and behind a proxy", async ({
	page,
}) => {
	await logIn(page, "/settings/server/network");
	await page.getByLabel("Public address").fill("https://photon.example.com");
	await page.getByLabel("Trusted proxies").fill("172.16.0.0/12");
	await page.getByLabel("Discovery").click();
	await page.getByRole("option", { name: "Off" }).click();
	await expectAccessible(page);
	const saved = page.waitForRequest(
		(r) => r.method() === "PUT" && r.url().endsWith("/api/v1/admin/network"),
	);
	await page.getByRole("button", { name: "Save" }).click();
	expect((await saved).postDataJSON()).toMatchObject({
		public_url: "https://photon.example.com",
		trusted_proxies: ["172.16.0.0/12"],
		discovery: "off",
	});
	await expect(page.getByText(/^Saved/)).toBeVisible();

	await page.reload();
	await expect(page.getByLabel("Public address")).toHaveValue(
		"https://photon.example.com",
	);
	await expect(page.getByLabel("Trusted proxies")).toHaveValue("172.16.0.0/12");
	await expect(page.getByLabel("Discovery")).toHaveText("Off");
});

test("a filtered library is kept as a smart collection, and its filters changed there", async ({
	page,
}) => {
	await logIn(page, "/libraries/l-films?mark=unwatched");
	await expect(
		page.getByRole("button", { name: "Save as a smart collection" }),
	).toHaveCount(0);

	await page.goto("/libraries/l-films?sort=added&genre=Drama");
	await page
		.getByRole("button", { name: "Save as a smart collection" })
		.click();
	await page.getByLabel("Name").fill("Dramas");
	await page.getByRole("button", { name: "Save", exact: true }).click();
	await expect(page).toHaveURL("/settings/server/collections/t-smart");
	await expect(page.getByText(/A smart collection/)).toBeVisible();
	await expectAccessible(page);

	await page.getByRole("link", { name: "Change its filters" }).click();
	await expect(page).toHaveURL(
		"/libraries/l-films?sort=added&genre=Drama&collection=t-smart",
	);
	const saved = page.waitForRequest(
		(r) => r.method() === "PUT" && r.url().endsWith("/t-smart/rule"),
	);
	await page.getByRole("button", { name: "Save to the collection" }).click();
	expect((await saved).postDataJSON()).toMatchObject({
		filter: { genres: ["Drama"] },
		sort: "added",
	});
	await expect(page).toHaveURL("/settings/server/collections/t-smart");
});

test("a collection is made from a TMDB list, and read again as asked", async ({
	page,
}) => {
	await logIn(page, "/settings/server/collections");
	await page.getByLabel("New collection").fill("Alien films");
	await page.getByLabel("Titles").click();
	await page.getByRole("option", { name: "A TMDB list" }).click();
	await page.getByLabel("List", { exact: true }).fill("8136");
	const made = page.waitForRequest(
		(r) => r.method() === "POST" && r.url().endsWith("/admin/collections"),
	);
	await page.getByRole("button", { name: "Make it" }).click();
	expect((await made).postDataJSON().list).toEqual({
		source: "tmdb",
		id: "8136",
	});
	await expect(page).toHaveURL("/settings/server/collections/t-smart");
	await expect(page.locator("main")).toContainText(
		"Holds the titles of TMDB list 8136 the library has",
	);
	await expect(page.locator("main")).toContainText(
		"2 of it are not in the library.",
	);
	await expectAccessible(page);

	await page.getByRole("button", { name: "Sync now" }).click();
	await expect(page.getByText("Read again.")).toBeVisible();
});

test("a collection made here is filled from its library", async ({ page }) => {
	await logIn(page, "/settings/server/collections");
	await expect(page.getByText("Made here", { exact: true })).toBeVisible();
	await expectAccessible(page);
	await page.getByRole("link", { name: /Lighthouse Films/ }).click();
	await page.getByRole("searchbox", { name: "Find a title" }).fill("quiet");
	await page.getByRole("button", { name: "Find" }).click();
	await page.getByRole("button", { name: "Add Quiet Hours" }).click();
	await expect(
		page.getByRole("link", { name: "Quiet Hours (2018)" }),
	).toBeVisible();
	await expectAccessible(page);
	await page.getByRole("button", { name: "Save the order" }).click();
	await expect(page.getByText("Saved.")).toBeVisible();
});

test("a title is edited, matched, given a poster and marked", async ({
	page,
}) => {
	await logIn(page, "/settings/server/titles/t-quiet");
	await expect(page.getByText("Matched to tmdb 101.")).toBeVisible();
	await expect(
		page.getByRole("button", { name: /tmdb · en · 1000×1500 · chosen/ }),
	).toBeVisible();
	await expectAccessible(page);

	await page
		.getByRole("textbox", { name: "Tagline" })
		.fill("Keep the light on.");
	await page.getByRole("button", { name: "Save", exact: true }).click();
	await expect(page.getByText("Saved.")).toBeVisible();
	await page.getByRole("textbox", { name: "Overview" }).fill("");
	await page.getByRole("button", { name: "Save", exact: true }).click();
	await expect(page.getByText("A field can't be emptied.")).toBeVisible();

	await page.getByRole("button", { name: "Search" }).click();
	await page.getByRole("button", { name: "Select Quiet Hours" }).click();
	await expect(
		page.getByText("Matched. Its details follow in a moment."),
	).toBeVisible();

	const posters = page
		.getByRole("tabpanel")
		.getByRole("button", { pressed: false });
	await posters.first().click();
	await expect(page.getByText("Chosen.")).toBeVisible();

	await page.getByRole("button", { name: "Add a stretch" }).click();
	await page.getByLabel("Starts").last().fill("0:30");
	await page.getByLabel("Ends").last().fill("1:45");
	await page.getByLabel("No recap").click();
	await page.getByRole("button", { name: "Save markers" }).click();
	await expect(page.getByText("Markers saved.")).toBeVisible();
});

test("a title is edited and its match fixed from its card", async ({
	page,
}) => {
	await logIn(page);
	const row = page.getByRole("region", { name: "Continue Watching" });
	await row.getByRole("button", { name: "More for Quiet Hours" }).click();
	await page.getByRole("menuitem", { name: "Edit…" }).click();
	const dialog = page.getByRole("dialog", { name: "Edit Quiet Hours" });
	await dialog
		.getByRole("textbox", { name: "Tagline" })
		.fill("Keep the light on.");
	await dialog.getByRole("button", { name: "Save", exact: true }).click();
	await expect(page.getByText("Saved.")).toBeVisible();
	await expectAccessible(page);
	await page.keyboard.press("Escape");

	await row.getByRole("button", { name: "More for Quiet Hours" }).click();
	await page.getByRole("menuitem", { name: "Fix match…" }).click();
	await expect(dialog.getByRole("tab", { name: "Match" })).toHaveAttribute(
		"aria-selected",
		"true",
	);
	await dialog.getByRole("button", { name: "Search" }).click();
	await expect(
		dialog.getByText("A night shift at a radio station."),
	).toBeVisible();
	await dialog.getByRole("button", { name: "Select Quiet Hours" }).click();
	await expect(
		page.getByText("Matched. Its details follow in a moment."),
	).toBeVisible();
});

test("a member's card offers no editing", async ({ page }) => {
	await asKids(page);
	await page.goto("/");
	await page
		.getByRole("region", { name: "Recently Added in Films" })
		.getByRole("button", { name: /^More for / })
		.first()
		.click();
	await expect(page.getByRole("menuitem", { name: "Edit…" })).toHaveCount(0);
});

test("a title is analysed from its card", async ({ page }) => {
	await logIn(page);
	await page
		.getByRole("region", { name: "Recently Added in Films" })
		.getByRole("button", { name: "More for Quiet Hours" })
		.click();
	await page.getByRole("menuitem", { name: "Analyse" }).click();
	await expect(
		page.getByText("Analysing Quiet Hours: its files are read again."),
	).toBeVisible();
});

test("a title's metadata is refreshed from its card, as much as is chosen", async ({
	page,
}) => {
	await logIn(page);
	await page
		.getByRole("region", { name: "Recently Added in Films" })
		.getByRole("button", { name: "More for Quiet Hours" })
		.click();
	await page.getByRole("menuitem", { name: "Refresh metadata…" }).click();
	const dialog = page.getByRole("dialog", { name: "Refresh metadata" });
	await expect(
		dialog.getByText("Choose how much of Quiet Hours"),
	).toBeVisible();
	await expectAccessible(page);

	const asked = page.waitForRequest("**/api/v1/admin/titles/t-film/refresh");
	await dialog.getByRole("button", { name: /Refresh all metadata/ }).click();
	expect((await asked).postDataJSON()).toEqual({ mode: "all" });
	await expect(
		page.getByText("Fetching all metadata for Quiet Hours again."),
	).toBeVisible();
	await expect(dialog).toBeHidden();
});

test("a title is unmatched from its card", async ({ page }) => {
	await logIn(page);
	await page
		.getByRole("region", { name: "Recently Added in Films" })
		.getByRole("button", { name: "More for Quiet Hours" })
		.click();
	await page.getByRole("menuitem", { name: "Unmatch" }).click();
	await expect(
		page.getByText("Quiet Hours was unmatched", { exact: false }),
	).toBeVisible();
});

test("a film of two copies is split apart from its card, once asked", async ({
	page,
}) => {
	await logIn(page);
	await page
		.getByRole("region", { name: "Recently Added in Films" })
		.getByRole("button", { name: "More for Quiet Hours" })
		.click();
	await page.getByRole("menuitem", { name: "Split apart…" }).click();
	const asked = page.getByRole("alertdialog", {
		name: "Split Quiet Hours apart?",
	});
	await expectAccessible(page);
	await asked.getByRole("button", { name: "Split apart" }).click();
	await expect(page.getByText("Quiet Hours was split apart.")).toBeVisible();
});

test("deleting from a card asks first, and says why its library refuses", async ({
	page,
}) => {
	await logIn(page);
	await page
		.getByRole("region", { name: "Recently Added in Films" })
		.getByRole("button", { name: "More for Quiet Hours" })
		.click();
	await page.getByRole("menuitem", { name: "Delete…" }).click();
	await page
		.getByRole("alertdialog", { name: "Delete Quiet Hours?" })
		.getByRole("button", { name: "Delete" })
		.click();
	await expect(
		page.getByText(
			"its library does not allow its titles' files to be deleted",
			{
				exact: false,
			},
		),
	).toBeVisible();
});

test("a library is set to ask in a language of its own", async ({ page }) => {
	let sent: Record<string, unknown> = {};
	page.on("request", (r) => {
		if (
			r.method() === "PATCH" &&
			r.url().endsWith("/admin/libraries/l-films")
		) {
			sent = r.postDataJSON();
		}
	});
	await logIn(page, "/settings/server/libraries/l-films");
	await expect(page.getByLabel("Metadata language")).toContainText(
		"Server default (British English)",
	);
	await page.getByLabel("Metadata language").click();
	await page.getByRole("option", { name: "German (Germany)" }).click();
	await expectAccessible(page);
	await page.getByRole("button", { name: "Save", exact: true }).click();
	await expect.poll(() => sent.metadata_language).toBe("de-DE");
	expect(sent.certification_country).toBe("");
});

test("a title is set to ask in a language of its own", async ({ page }) => {
	let sent: Record<string, unknown> = {};
	page.on("request", (r) => {
		if (r.method() === "PUT" && r.url().endsWith("/titles/t-quiet/locale")) {
			sent = r.postDataJSON();
		}
	});
	await logIn(page, "/settings/server/titles/t-quiet");
	await page.getByLabel("Metadata language").click();
	await page.getByRole("option", { name: "French (France)" }).click();
	await page.getByRole("button", { name: "Save language" }).click();
	await expect(page.getByText("Saved. It is described again")).toBeVisible();
	expect(sent).toEqual({
		metadata_language: "fr-FR",
		certification_country: "",
	});
});
