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
		.getByRole("link", { name: "Recently Added in Films View all" })
		.click();
	await expect(page).toHaveURL("/libraries/l-films?sort=added");
	await expect(
		page.getByRole("heading", { level: 1, name: "Films" }),
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

test("a library offers its collections only where it keeps some", async ({
	page,
}) => {
	await logIn(page, "/libraries/l-films");
	const views = page.getByRole("navigation", { name: "Films views" });
	await views.getByRole("link", { name: "Collections" }).click();
	await expect(page.getByRole("list", { name: "Collections" })).toBeVisible();
	await page.goto("/libraries/l-shows");
	await expect(
		page
			.getByRole("navigation", { name: "Shows views" })
			.getByRole("link", { name: "Collections" }),
	).toHaveCount(0);
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

test("a wall follows the server as it finds titles", async ({ page }) => {
	await logIn(page, "/libraries/l-films");
	await expect(page.getByText("250 titles")).toBeVisible();
	const emit = (event: string, data: object, add?: string) =>
		page.request.post("/mock/emit", { data: { event, data, add } });
	await expect
		.poll(async () => (await page.request.get("/mock/listening")).json())
		.toBeGreaterThan(0);
	await emit(
		"library.changed",
		{ kind: "library.changed", library_id: "l-films" },
		"Aardvark",
	);
	await expect(page.getByText("251 titles")).toBeVisible();
	await expect(page.getByRole("link", { name: /Aardvark/ })).toBeVisible();
});

test("the bar says what the server is doing: a scan as it goes, the tasks running and a backlog counting down", async ({
	page,
}) => {
	await logIn(page, "/libraries/l-films");
	await page.getByRole("button", { name: /^Activity/ }).click();
	const menu = page.getByRole("dialog", { name: "Activity" });
	const scan = menu.getByRole("status");
	await expect(scan).toContainText("Scanning Films");
	await expect(scan).toContainText(/Reading folders: \d+ of 40/);
	await expect(scan).toContainText("Heat (1995)");
	await expect(
		menu.getByRole("list", { name: "Scheduled tasks running" }),
	).toContainText("Scan libraries");
	const jobs = menu.getByRole("list", { name: "Jobs running" });
	await expect(jobs).toContainText("Make previews");
	await expect(jobs).toContainText(/[\d,]+ of 8,607/);
	await expect(jobs).not.toContainText("585 of 8,607");
	await expect(
		jobs.getByRole("progressbar", { name: /^Make previews: [\d,]+ of 8,607$/ }),
	).toBeVisible();
	await expectAccessible(page);
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
	// The 1080p copy is in two files: each is a download of its own.
	await expect(page.getByText(/2 downloads requested/)).toBeVisible();

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
	await expect(page.getByRole("link", { name: /Second/ })).toBeVisible();
	await expectAccessible(page);
});

test("an episode's page lists its whole season to pick another", async ({
	page,
}) => {
	await logIn(page, "/titles/t-ep");
	await expect(page.getByRole("heading", { name: "Season 1" })).toBeVisible();
	await expect(page.getByRole("link", { name: /Pilot/ })).toHaveAttribute(
		"aria-current",
		"page",
	);
	await page.getByRole("link", { name: /Second/ }).click();
	await expect(page).toHaveURL("/titles/t-ep2");
	await expect(page.getByRole("link", { name: /Second/ })).toHaveAttribute(
		"aria-current",
		"page",
	);
	await expectAccessible(page);
});

test("a picture stands in as its blur until it arrives", async ({ page }) => {
	let arrive = () => {};
	const held = new Promise<void>((resolve) => {
		arrive = resolve;
	});
	await page.route("**/api/v1/artwork/**", async (route) => {
		await held;
		await route.continue();
	});
	await logIn(page, "/titles/t-film");
	const backdrop = page.locator('img[fetchpriority="high"]');
	// The blur is the frame's, so the picture can fade in over it.
	await expect(backdrop.locator("..")).toHaveCSS(
		"background-image",
		/^url\("data:image\/png/,
	);
	expect(await backdrop.evaluate((img: HTMLImageElement) => img.complete)).toBe(
		false,
	);
	await expectAccessible(page);
	arrive();
	await expect
		.poll(() => backdrop.evaluate((img: HTMLImageElement) => img.naturalWidth))
		.toBeGreaterThan(0);
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
	// A run of results narrowed to a library leads back to them so narrowed.
	await page.goto("/search/movie?q=quiet&library=l-films");
	await expect(page.getByRole("link", { name: /Results for/ })).toHaveAttribute(
		"href",
		"/search?q=quiet&library=l-films",
	);
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
	// Answered, the question closes, and the page it leads to can be used.
	await expect(page.getByRole("alertdialog")).toHaveCount(0);
	await expect(page).toHaveURL("/playlists");
	await expect(page.getByText("No playlists yet.")).toBeVisible();
	await page.getByLabel("New playlist").click();
	await expect(page.getByLabel("New playlist")).toBeFocused();
});

test("a playlist made on the playlists page is named as it was typed", async ({
	page,
}) => {
	await logIn(page, "/playlists");
	await page.getByLabel("New playlist").fill("Rainy days");
	await page.getByRole("button", { name: "Create" }).click();
	await expect(page.getByRole("heading", { name: "Rainy days" })).toBeVisible();
	await page.getByRole("button", { name: "Delete" }).click();
	await page
		.getByRole("alertdialog")
		.getByRole("button", { name: "Delete" })
		.click();
	await expect(page.getByText("No playlists yet.")).toBeVisible();
});

test("a title put on the watchlist is on its home row and its page", async ({
	page,
}) => {
	await logIn(page, "/watchlist");
	await expect(page.getByText(/Nothing here yet/)).toBeVisible();
	await expectAccessible(page);

	await page.goto("/titles/t-film");
	const watchlist = page.getByRole("button", { name: "Watchlist" });
	await expect(watchlist).toHaveAttribute("aria-pressed", "false");
	await watchlist.click();
	await expect(watchlist).toHaveAttribute("aria-pressed", "true");
	await page.goto("/");
	const row = page.getByRole("region", { name: "Watchlist" });
	await expect(row.getByRole("link", { name: /Quiet Hours/ })).toBeVisible();

	await page
		.getByRole("navigation")
		.getByRole("link", { name: "Watchlist" })
		.click();
	await expect(page).toHaveURL("/watchlist");
	// The address changes before home, where it is on every row, is drawn over.
	await expect(
		page.getByRole("heading", { level: 1, name: "Watchlist" }),
	).toBeVisible();
	await expect(page.getByRole("link", { name: /Quiet Hours/ })).toBeVisible();
	await expectAccessible(page);
});

test("a home row's own page lists the row", async ({ page }) => {
	await logIn(page, "/home/continue_watching");
	await expect(
		page.getByRole("heading", { level: 1, name: "Continue Watching" }),
	).toBeVisible();
	await expect(page.getByRole("link", { name: /Pilot/ })).toBeVisible();
	await expectAccessible(page);
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
	await expect(
		page.getByRole("link", { name: "Save Quiet Hours, part 2 of 2" }).first(),
	).toBeVisible();
	await expectAccessible(page);
});

test("a copy in two files is downloaded a file at a time", async ({ page }) => {
	const asked: (string | undefined)[] = [];
	page.on("request", (r) => {
		if (r.method() === "POST" && r.url().endsWith("/api/v1/downloads")) {
			asked.push(r.postDataJSON().part_id);
		}
	});
	await logIn(page, "/titles/t-film");
	await page.getByLabel("Version").click();
	await page.getByRole("option", { name: /1080p/ }).click();
	await page.getByRole("button", { name: "More", exact: true }).click();
	await page.getByRole("menuitem", { name: "Download…" }).click();
	const dialog = page.getByRole("dialog", { name: "Download" });
	await expect(
		dialog.getByRole("radio", { name: "All 2 files" }),
	).toBeChecked();
	await dialog.getByRole("button", { name: "Download", exact: true }).click();
	await expect(
		page.getByText("2 downloads requested, one a file."),
	).toBeVisible();
	expect(asked.toSorted()).toEqual(["p-hd", "p-hd2"]);

	await page.getByRole("button", { name: "More", exact: true }).click();
	await page.getByRole("menuitem", { name: "Download…" }).click();
	await dialog.getByRole("radio", { name: /Part 2/ }).check();
	await dialog.getByRole("button", { name: "Download", exact: true }).click();
	await expect(page.getByText("Download requested.")).toBeVisible();
	expect(asked.at(-1)).toBe("p-hd2");
	await expectAccessible(page);
});

test("a title's link is copied to share, or shown where it cannot be", async ({
	page,
	context,
}) => {
	await context.grantPermissions(["clipboard-read", "clipboard-write"]);
	await logIn(page);
	// A row no other test changes.
	const row = page.getByRole("region", { name: "Recently Added in Films" });
	await row.getByRole("button", { name: "More for Quiet Hours" }).click();
	await page.getByRole("menuitem", { name: "Share…" }).click();
	await expect(
		page.getByText("The link to Quiet Hours was copied."),
	).toBeVisible();
	expect(await page.evaluate(() => navigator.clipboard.readText())).toMatch(
		/\/titles\/t-film$/,
	);

	// A server reached by its address on the LAN is no secure page: there is
	// neither a share sheet nor a clipboard to write to.
	await page.evaluate(() => {
		Object.defineProperty(navigator, "clipboard", { value: undefined });
		Object.defineProperty(navigator, "share", { value: undefined });
	});
	await row.getByRole("button", { name: "More for Quiet Hours" }).click();
	await page.getByRole("menuitem", { name: "Share…" }).click();
	const dialog = page.getByRole("dialog", { name: "Share Quiet Hours" });
	await expect(dialog.getByRole("textbox", { name: "Link" })).toHaveValue(
		/\/titles\/t-film$/,
	);
	await expectAccessible(page);
});

test("an episode's card leads to its season", async ({ page }) => {
	await logIn(page, "/titles/t-ep");
	await page.getByRole("button", { name: "Favourite" }).click();
	await page.goto("/");
	await page
		.getByRole("region", { name: "Favourites" })
		.getByRole("button", { name: "More for Small Show: Pilot" })
		.click();
	await page.getByRole("menuitem", { name: "Go to Season 1" }).click();
	await expect(page).toHaveURL("/titles/t-s1");
	// The server is the other tests' too: the episode is as it was.
	await page.request.delete("/api/v1/titles/t-ep/favourite");
});

test("a version is chosen from a card to play or to download", async ({
	page,
}) => {
	await logIn(page);
	const row = page.getByRole("region", { name: "Recently Added in Films" });
	await row.getByRole("button", { name: "More for Quiet Hours" }).click();
	await page.getByRole("menuitem", { name: "Play version…" }).click();
	await page
		.getByRole("dialog", { name: "Play version" })
		.getByRole("button", { name: /1080p/ })
		.click();
	await expect(page).toHaveURL(/\/play\/t-film\?.*version=v-hd/);

	await page.goto("/");
	await row.getByRole("button", { name: "More for Quiet Hours" }).click();
	await page.getByRole("menuitem", { name: "Download…" }).click();
	await page
		.getByRole("dialog", { name: "Download version" })
		.getByRole("button", { name: /4K/ })
		.click();
	await page
		.getByRole("dialog", { name: "Download" })
		.getByRole("button", { name: "Download" })
		.click();
	await expect(
		page.getByText("Download requested. It's in Downloads."),
	).toBeVisible();
});

test("a reader finds subtitles for a film and adds one", async ({ page }) => {
	let added: Record<string, unknown> | undefined;
	page.on("request", (r) => {
		if (r.method() === "POST" && r.url().endsWith("/t-film/subtitles")) {
			added = r.postDataJSON();
		}
	});
	await logIn(page, "/titles/t-film");
	await page.getByRole("button", { name: "More", exact: true }).click();
	await page.getByRole("menuitem", { name: "Find subtitles…" }).click();
	const dialog = page.getByRole("dialog", { name: "Find subtitles" });
	await expect(dialog.getByLabel("Made for this file")).toBeVisible();
	await expect(dialog).toContainText("Quiet.Hours.2160p");
	await expectAccessible(page);
	await dialog.getByRole("button", { name: "Add" }).click();
	await expect(page.getByText(/added to Quiet Hours/)).toBeVisible();
	expect(added).toMatchObject({
		version_id: "v-4k",
		source: "opensubtitles",
		id: "9",
	});
});
