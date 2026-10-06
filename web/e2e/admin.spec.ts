import { expect, type Page, test } from "@playwright/test";
import { expectAccessible, logIn } from "./helpers";

// Dialogs open without animating, so a check never reads one half faded in.
test.use({ reducedMotion: "reduce" });

async function asKids(page: Page) {
	await logIn(page);
	await page.goto("/profiles");
	await page.getByRole("button", { name: "Kids" }).click();
	await expect(
		page.getByRole("button", { name: "Kids's profile" }),
	).toBeVisible();
}

test("the overview follows the server live: who is playing, and a scan as it grows", async ({
	page,
}) => {
	await logIn(page, "/admin");
	await expect(page.getByRole("heading", { name: "Den" })).toBeVisible();

	const card = page.locator("article", { hasText: "Quiet Hours" });
	await expect(card).toContainText("Kids · Living Room · Photon for tvOS");
	await expect(card).toContainText("Transcode");
	await expect(card).toContainText("Hardware · VideoToolbox");
	await expect(card).toContainText("Because of the video codec");
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
	await expect(
		page.getByText("Answers apps looking on the network"),
	).toBeVisible();
	await expectAccessible(page);

	await card.getByRole("button", { name: "Stop this play" }).click();
	await page
		.getByRole("alertdialog")
		.getByRole("button", { name: "Stop" })
		.click();
	await expect(page.getByText("Kids's play was stopped.")).toBeVisible();
});

test("a member is told the dashboard is not theirs", async ({ page }) => {
	await asKids(page);
	await page.goto("/admin");
	await expect(
		page.getByRole("heading", { name: "only an admin may" }),
	).toBeVisible();
	await expect(page.getByText("403")).toBeVisible();
});

test("a library is added from a folder found by browsing the server", async ({
	page,
}) => {
	await logIn(page, "/admin/libraries");
	await expect(page.getByRole("heading", { name: "Films" })).toBeVisible();
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
	await page.getByRole("button", { name: "Trust TMDB more" }).click();
	await expect(
		page.getByRole("listitem").filter({ hasText: "TMDB" }).first(),
	).toBeVisible();
	await expectAccessible(page);
	await page.getByRole("button", { name: "Add and scan" }).click();
	await expect(page).toHaveURL("/admin/libraries");

	await page.getByRole("link", { name: "Edit Films" }).click();
	await expect(page.getByRole("textbox", { name: "Folder" })).toHaveValue(
		"/media/films",
	);
	await expectAccessible(page);
	await page.getByRole("button", { name: "Save" }).click();
	await expect(page.getByText("Saved.")).toBeVisible();
});

test("a library's metadata is refreshed, what is missing or all of it", async ({
	page,
}) => {
	await logIn(page, "/admin/libraries");
	await page.getByRole("button", { name: "Refresh metadata of Films" }).click();
	const dialog = page.getByRole("dialog", { name: "Refresh library metadata" });
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
		page.getByText("Films is being filled in where it's missing."),
	).toBeVisible();
	await expect(dialog).toBeHidden();

	await page.getByRole("button", { name: "Refresh metadata of Films" }).click();
	await dialog.getByRole("button", { name: "Cancel" }).click();
	await expect(dialog).toBeHidden();
});

test("a profile is added, and what another may see is set", async ({
	page,
}) => {
	await logIn(page, "/admin/profiles");
	await expectAccessible(page);
	await page.getByRole("button", { name: "Add a profile" }).click();
	await page.getByRole("textbox", { name: "Name" }).fill("Guest");
	await page.getByRole("button", { name: "Add profile" }).click();
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
	await logIn(page, "/admin/providers");
	await expect(page.getByText("Needs settings").first()).toBeVisible();
	await expectAccessible(page);
	await page.getByLabel("API key (required)").fill("abc123");
	await page.getByRole("button", { name: "Save MDBList settings" }).click();
	await expect(page.getByText("Saved.")).toBeVisible();
});

test("tasks run, dead jobs are retried, and the logs read", async ({
	page,
}) => {
	await logIn(page, "/admin/tasks");
	await expect(page.getByText("Failed: disk full")).toBeVisible();
	await expectAccessible(page);
	await page
		.getByRole("button", { name: "Run now Back up the database" })
		.click();
	await expect(
		page.getByText("Back up the database is running."),
	).toBeVisible();

	await page.goto("/admin/jobs");
	await expect(page.getByText("TMDB said 503")).toBeVisible();
	await expectAccessible(page);
	await page.getByRole("button", { name: "Try again job 41" }).click();
	await expect(page.getByText("The job is queued again.")).toBeVisible();
	await expect(page.getByText("Nothing has been given up on.")).toBeVisible();

	await page.goto("/admin/activity?kind=library.added");
	await expect(page.getByText("Library Films was added")).toBeVisible();
	await expect(page.getByText("Back up the database failed")).toHaveCount(0);
	await expectAccessible(page);

	await page.goto("/admin/history");
	await expect(page.getByRole("cell", { name: "Kids" })).toBeVisible();
	await expect(page.getByRole("cell", { name: "100%" })).toBeVisible();
	await expectAccessible(page);
});

test("a webhook's secret is shown once, to copy", async ({ page }) => {
	await logIn(page, "/admin/webhooks");
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

test("a collection made here is filled from its library", async ({ page }) => {
	await logIn(page, "/admin/collections");
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
	await logIn(page, "/admin/titles/t-quiet");
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
	await page.getByRole("button", { name: "This one : Quiet Hours" }).click();
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
