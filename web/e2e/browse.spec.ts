import { expect, test } from "@playwright/test";
import { expectAccessible, logIn } from "./helpers";

test("a title is taken off Continue Watching from its card", async ({
	page,
}) => {
	await logIn(page);
	const row = page.getByRole("region", { name: "Continue Watching" });
	await expect(row.getByRole("link", { name: /Quiet Hours/ })).toBeVisible();
	await row.getByRole("button", { name: "More for Quiet Hours" }).click();
	await page
		.getByRole("menuitem", { name: "Remove from Continue Watching" })
		.click();
	await expect(page.getByText("Removed from Continue Watching.")).toBeVisible();
	await expect(row.getByRole("link", { name: /Quiet Hours/ })).toHaveCount(0);

	// A row shown whole leads nowhere; one with more leads to the rest.
	await expect(
		page.getByRole("link", { name: /Continue Watching/ }),
	).toHaveCount(0);
	await page
		.getByRole("link", { name: "Recently Added Films View all" })
		.click();
	await expect(page).toHaveURL("/home/recently_added_films");
	await expect(
		page.getByRole("heading", { name: "Recently Added Films" }),
	).toBeVisible();
});

test("a rail shows twenty, and its heading leads to all of them", async ({
	page,
}) => {
	await logIn(page, "/titles/t-film");
	const similar = page.getByRole("region", { name: /More like this/ });
	await expect(similar.getByRole("listitem")).toHaveCount(20);
	await similar.getByRole("link", { name: "More like this View all" }).click();
	await expect(page).toHaveURL("/titles/t-film/similar");
	await expect(
		page.getByRole("heading", { level: 1, name: "More like this" }),
	).toBeVisible();
	await expect(page.getByRole("main").getByRole("listitem")).toHaveCount(25);
	await expectAccessible(page);

	// A rail that shows everything has no way to more.
	await page.goto("/titles/t-film");
	await expect(
		page.getByRole("link", { name: /Cast & crew View all/ }),
	).toHaveCount(0);
	await page.goto("/titles/t-film/cast");
	await expect(page.getByRole("link", { name: /Ada Lane/ })).toBeVisible();
	await page.goto("/people/5f0c1d8e-2b1a-4c3d-9e8f-0a1b2c3d4e5f/acting");
	await expect(page.getByText("2018 · Host, Director")).toBeVisible();
});

test("a library's wall pages as it scrolls and jumps to a letter", async ({
	page,
}) => {
	await logIn(page, "/libraries/l-films");
	await expect(page.getByText("250 titles")).toBeVisible();
	await expectAccessible(page);
	const wall = page.getByRole("list", { name: "Films" });
	await expect(wall.getByRole("link", { name: /Hilm 244/ })).toHaveCount(0);

	await page.getByRole("button", { name: "Titles starting with Q" }).click();
	await expect(page.getByRole("link", { name: /Quiet Hours/ })).toBeFocused();

	await page.mouse.wheel(0, 100_000);
	await expect(wall.getByRole("link", { name: /Hilm 244/ })).toBeVisible();
});

test("a wall is sorted, filtered and drawn as the reader asks", async ({
	page,
}) => {
	await logIn(page, "/libraries/l-films");
	await page.getByRole("button", { name: /Sort by/ }).click();
	await page.getByRole("menuitemradio", { name: "Date added" }).click();
	await expect(page).toHaveURL("/libraries/l-films?sort=added");
	await page.keyboard.press("Escape");

	await page.getByRole("button", { name: "Filter" }).click();
	await page.getByRole("menuitem", { name: "Genre" }).click();
	await page.getByRole("menuitemcheckbox", { name: "Drama" }).click();
	await expect(page).toHaveURL("/libraries/l-films?sort=added&genre=Drama");
	await expect(page.getByText("1 title", { exact: true })).toBeVisible();
	await page.keyboard.press("Escape");
	await page.keyboard.press("Escape");
	await page.getByRole("button", { name: "Clear filters" }).click();
	await expect(page).toHaveURL("/libraries/l-films?sort=added");

	await page.getByRole("radio", { name: "List" }).click();
	await page.reload();
	await expect(page.getByRole("radio", { name: "List" })).toBeChecked();
	await expectAccessible(page);
	await page.getByRole("radio", { name: "Posters" }).click();
});

test("a wall follows the server: a scan's progress in the bar, then what it found", async ({
	page,
}) => {
	await logIn(page, "/libraries/l-films");
	await expect(page.getByText("250 titles")).toBeVisible();
	const emit = (event: string, data: object, add?: string) =>
		page.request.post("/mock/emit", { data: { event, data, add } });
	await expect
		.poll(async () => (await page.request.get("/mock/listening")).json())
		.toBeGreaterThan(0);

	await emit("scan.progress", {
		kind: "scan.progress",
		library_id: "l-films",
		details: { phase: "reading", done: 40, known: 100, folder: "Heat (1995)" },
	});
	const activity = page.getByRole("button", { name: /^Activity/ });
	await activity.click();
	const scan = page.getByRole("menu").getByRole("status");
	await expect(scan).toContainText("Scanning Films");
	await expect(scan).toContainText("Reading folders: 40 of 100");
	await expect(scan).toContainText("Heat (1995)");
	await page.keyboard.press("Escape");
	await emit(
		"library.changed",
		{ kind: "library.changed", library_id: "l-films" },
		"Aardvark",
	);
	await expect(page.getByText("251 titles")).toBeVisible();
	await expect(page.getByRole("link", { name: /Aardvark/ })).toBeVisible();
	await emit("library.scanned", {
		kind: "library.scanned",
		library_id: "l-films",
	});
	await expect(activity).toHaveCount(0);
});

test("a film's page plays the copy and tracks chosen", async ({ page }) => {
	await logIn(page, "/titles/t-film");
	await expect(
		page.getByRole("heading", { level: 1, name: "Quiet Hours" }),
	).toBeVisible();
	await expect(page.getByText("Nobody is listening.")).toBeVisible();
	const ratings = page.getByRole("list", { name: "Ratings" });
	await expect(ratings.getByRole("img", { name: "IMDb" })).toBeVisible();
	await expect(ratings).toContainText("7.8");
	await expectAccessible(page);

	const play = page.getByRole("link", { name: /^Play$|^Resume/ });
	await expect(play).toHaveAttribute("href", "/play/t-film?version=v-4k");
	await page.getByRole("button", { name: "Audio" }).click();
	await page.getByRole("option", { name: /Commentary/ }).click();
	await expect(play).toHaveAttribute(
		"href",
		"/play/t-film?version=v-4k&audio=2",
	);
	await page.getByRole("button", { name: "Version" }).click();
	await page.getByRole("option", { name: /1080p/ }).click();
	await expect(play).toHaveAttribute("href", "/play/t-film?version=v-hd");
	await expect(page.getByRole("button", { name: "Audio" })).toHaveCount(0);

	const favourite = page.getByRole("button", { name: "Favourite" });
	await expect(favourite).toHaveAttribute("aria-pressed", "false");
	await favourite.click();
	await expect(favourite).toHaveAttribute("aria-pressed", "true");

	await page.getByRole("button", { name: "More", exact: true }).click();
	await page.getByRole("menuitem", { name: "Media info" }).click();
	const info = page.getByRole("dialog", { name: "Media info" });
	await expect(info).toContainText("TRUEHD");
	await expect(info).toContainText("Sign on");
	await expect(page.getByRole("menu")).toHaveCount(0);
	await expectAccessible(page);
	await page.keyboard.press("Escape");

	await page.getByRole("button", { name: "More", exact: true }).click();
	await page.getByRole("menuitem", { name: "Download…" }).click();
	await page.getByRole("radio", { name: /720p/ }).check();
	await page.getByRole("button", { name: "Download", exact: true }).click();
	await expect(page.getByText(/Download requested/)).toBeVisible();

	const extras = page.getByRole("region", { name: "Extras" });
	await expect(extras.getByRole("link", { name: /Trailer/ })).toHaveAttribute(
		"href",
		"/play/t-trailer",
	);
	await expect(
		extras.getByRole("link", { name: /Trailer/ }).locator("img"),
	).toHaveAttribute("src", "/api/v1/parts/p-trailer/chapters/0/image");
	const remote = extras.getByRole("link", { name: /Making Quiet Hours/ });
	await expect(remote).toHaveAttribute(
		"href",
		"https://www.youtube.com/watch?v=quiet",
	);
	// Its still comes from this server, never from YouTube.
	await expect(remote.locator("img")).toHaveAttribute(
		"src",
		/^\/api\/v1\/artwork\//,
	);
	const collections = page.getByRole("region", { name: "Collections" });
	await expect(
		collections.getByRole("link", { name: /Quiet Collection/ }),
	).toHaveAttribute("href", "/titles/c-set");
	await expect(collections.locator("img")).toBeVisible();

	// One card for one person, however many jobs they did, and performers first.
	const cast = page.getByRole("region", { name: "Cast & crew" });
	await expect(cast.getByRole("listitem")).toHaveCount(2);
	await expect(cast.getByRole("listitem").first()).toContainText(
		"Host, Creator, Director, Writer",
	);
	const details = page.getByRole("region", { name: "Details" });
	await expect(details.getByRole("link", { name: "Ada Lane" })).toHaveCount(2);

	await cast.getByRole("link", { name: /Ada Lane/ }).click();
	await expect(page.getByRole("heading", { name: "Ada Lane" })).toBeVisible();
	await expect(page.getByRole("heading", { name: "Acting" })).toBeVisible();
	// Acted in and directed: one card, under acting.
	await expect(page.getByRole("heading", { name: "Directing" })).toHaveCount(0);
	await expect(page.getByText("2018 · Host, Director")).toBeVisible();
	await expectAccessible(page);
});

test("a show plays where the reader is, and its season lists episodes", async ({
	page,
}) => {
	await logIn(page, "/titles/t-show");
	await expect(
		page.getByRole("link", { name: "Resume S1 E1" }),
	).toHaveAttribute("href", "/play/t-ep");
	await expect(page.getByRole("heading", { name: "Next up" })).toBeVisible();
	await page.getByRole("link", { name: /Season 1/ }).click();
	await expect(page).toHaveURL("/titles/t-s1");
	await expect(page.getByRole("link", { name: /2\. Second/ })).toBeVisible();
	await expectAccessible(page);
});

test("a box set lists its titles", async ({ page }) => {
	await logIn(page, "/titles/c-set");
	await expect(page.getByRole("heading", { name: "1 title" })).toBeVisible();
	await expect(page.getByRole("link", { name: /Quiet Hours/ })).toBeVisible();
});

test("search finds titles and people", async ({ page }) => {
	await logIn(page, "/search?q=ada");
	await expect(page.getByRole("heading", { name: "People" })).toBeVisible();
	await page.goto("/search?q=quiet");
	await expect(page.getByRole("heading", { name: "Films" })).toBeVisible();
	await expect(
		page.getByRole("heading", { name: "Collections" }),
	).toBeVisible();
	await expectAccessible(page);
});

test("a playlist is made, filled, reordered and deleted", async ({ page }) => {
	await logIn(page, "/titles/t-ep2");
	await page.getByRole("button", { name: "More", exact: true }).click();
	await page.getByRole("menuitem", { name: "Add to playlist…" }).click();
	await page.getByLabel("New playlist").fill("Late shows");
	await page.getByRole("button", { name: "Create" }).click();
	await expect(page.getByText("Added to Late shows.")).toBeVisible();

	await page.goto("/titles/t-ep");
	await page.getByRole("button", { name: "More", exact: true }).click();
	await page.getByRole("menuitem", { name: "Add to playlist…" }).click();
	await page.getByRole("button", { name: /Late shows/ }).click();
	await expect(page.getByText("Added to Late shows.")).toBeVisible();

	await page.goto("/playlists");
	await expectAccessible(page);
	await page.getByRole("link", { name: /Late shows/ }).click();
	await expect(page.getByRole("link", { name: "Play all" })).toHaveAttribute(
		"href",
		"/play/t-ep2?playlist=pl-1",
	);
	await page.getByRole("button", { name: "Move Small Show: Pilot up" }).click();
	await expect(page.getByRole("link", { name: "Play all" })).toHaveAttribute(
		"href",
		"/play/t-ep?playlist=pl-1",
	);
	await expectAccessible(page);

	await page.getByRole("button", { name: "Rename" }).click();
	await page.getByLabel("Name").fill("Night shift");
	await page.getByRole("button", { name: "Save" }).click();
	await expect(
		page.getByRole("heading", { name: "Night shift" }),
	).toBeVisible();

	await page.getByRole("button", { name: "Remove Small Show: Second" }).click();
	await expect(page.getByText("1 title")).toBeVisible();

	await page.getByRole("button", { name: "Delete" }).click();
	await page
		.getByRole("alertdialog")
		.getByRole("button", { name: "Delete" })
		.click();
	await expect(page).toHaveURL("/playlists");
	await expect(page.getByText("No playlists yet.")).toBeVisible();
});

test("favourites, history and downloads list the reader's own", async ({
	page,
}) => {
	await logIn(page, "/favourites");
	await expect(page.getByRole("heading", { name: "Films" })).toBeVisible();
	await expectAccessible(page);
	await page.goto("/history");
	await expect(page.getByText(/stopped at 10:00/)).toBeVisible();
	await expectAccessible(page);
	await page.goto("/downloads");
	await expect(page.getByRole("link", { name: /Save/ })).toBeVisible();
	await expectAccessible(page);
});
