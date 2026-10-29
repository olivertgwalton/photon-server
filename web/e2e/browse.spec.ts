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

	// Every row's heading leads to its page, a short row's too.
	await expect(
		page.getByRole("link", { name: "Continue Watching View all" }),
	).toBeVisible();
	await page
		.getByRole("link", { name: "Recently Added in Films View all" })
		.click();
	// A library's row opens what its rail holds, not the library's wall.
	await expect(page).toHaveURL("/libraries/l-films/rows/recently_added_films");
	await expect(
		page.getByRole("heading", { level: 1, name: "Recently Added in Films" }),
	).toBeVisible();
	await expect(page.getByRole("main").getByRole("listitem")).toHaveCount(20);
});

test("a phone's wall is as many posters across as the apps' at each card size", async ({
	page,
}) => {
	// An iPhone 16 Pro Max.
	await page.setViewportSize({ width: 440, height: 956 });
	await logIn(page);
	const across = async () => {
		await page.goto("/libraries/l-films");
		const cards = page.getByRole("main").getByRole("listitem");
		await expect(cards.first()).toBeVisible();
		return page.evaluate(() => {
			const tops = [
				...document.querySelectorAll(
					"main li:has(> div > a[href^='/titles/'])",
				),
			]
				.map((li) => li.getBoundingClientRect())
				.filter((r) => r.width > 0)
				.map((r) => Math.round(r.top));
			return tops.filter((top) => top === tops[0]).length;
		});
	};
	expect(await across()).toBe(3);
	for (const [size, count] of [
		["Compact", 4],
		["Large", 2],
	] as const) {
		await page.goto("/settings/home");
		await page.getByRole("combobox", { name: "Card size" }).click();
		await page.getByRole("option", { name: size }).click();
		await expect(page.getByText("Saved for this browser.")).toBeVisible();
		expect(await across()).toBe(count);
	}
});

test("only Continue Watching's cards say how far the reader is", async ({
	page,
}) => {
	await logIn(page);
	const watching = page.getByRole("region", { name: "Continue Watching" });
	await expect(
		watching.getByRole("progressbar", { name: "Watched" }).first(),
	).toBeVisible();
	// Quiet Hours is part way, and says so nowhere else.
	const added = page.getByRole("region", { name: "Recently Added in Films" });
	await expect(added.getByRole("link", { name: /Quiet Hours/ })).toBeVisible();
	await expect(added.getByRole("progressbar")).toHaveCount(0);
	await page.goto("/search?q=Quiet");
	await expect(
		page
			.getByRole("main")
			.getByRole("link", { name: /Quiet Hours/ })
			.first(),
	).toBeVisible();
	await expect(page.getByRole("progressbar")).toHaveCount(0);

	// Its own page draws posters, an episode by its place and show, and no
	// progress, as the apps' do.
	await page.goto("/home/continue_watching");
	const pilot = page.getByRole("link", { name: /Pilot/ });
	await expect(pilot).toContainText("S1, E1 · Small Show");
	await expect(pilot.locator(".aspect-2\\/3")).toHaveCount(1);
	await expect(page.getByRole("progressbar")).toHaveCount(0);
	await expectAccessible(page);
});

test("home's showcase wears what the reader opens, or slides through a row they chose", async ({
	page,
}) => {
	await logIn(page);
	const showcase = page.getByRole("region", { name: "Showcase" });
	// Curated, it is the first library row's first title, never the reader's.
	await expect(
		showcase.getByRole("heading", { name: "Quiet Hours" }),
	).toBeVisible();
	await expect(
		showcase.getByRole("link", {
			name: /^(Play|Resume [0-9:]+) Quiet Hours$/,
		}),
	).toBeVisible();
	await expectAccessible(page);

	// A title opened from a row is the one it wears on coming back.
	await page
		.getByRole("region", { name: "Continue Watching" })
		.getByRole("link", { name: /Pilot/ })
		.click();
	await expect(page).toHaveURL("/titles/t-ep");
	// Slowed, so the new title's words are still coming in when looked at.
	const cdp = await page.context().newCDPSession(page);
	await cdp.send("Animation.enable");
	await cdp.send("Animation.setPlaybackRate", { playbackRate: 0.02 });
	await page.goBack();
	await expect(
		showcase.getByRole("heading", { name: "Small Show" }),
	).toBeVisible();
	await expect(showcase.getByText("Pilot", { exact: true })).toBeVisible();
	// A pane of glass under a fading element blurs nothing behind it, so the
	// words fade and the panes holding them do not.
	const fading = await showcase.evaluate((section) => {
		const moving = (el: Element) =>
			el.getAnimations().some((a) => a.playState === "running");
		const panes = [...section.querySelectorAll("*")].filter(
			(el) => getComputedStyle(el).backdropFilter !== "none",
		);
		const under = panes.filter((pane) => {
			for (
				let el = pane.parentElement;
				el && el !== section;
				el = el.parentElement
			)
				if (moving(el)) return true;
			return moving(pane);
		});
		return {
			words: [...section.querySelectorAll("*")].filter(moving).length,
			panes: panes.length,
			under: under.length,
		};
	});
	expect(fading.words).toBeGreaterThan(0);
	expect(fading.panes).toBeGreaterThan(0);
	expect(fading.under).toBe(0);
	await cdp.send("Animation.setPlaybackRate", { playbackRate: 1 });

	// A row chosen in this browser is a carousel of its titles.
	await page.goto("/settings/home");
	await page.getByRole("combobox", { name: "Showcase shows" }).click();
	await page.getByRole("option", { name: "Recently Added in Films" }).click();
	await page.goto("/");
	await expect(showcase.locator("[aria-roledescription=slide]")).toHaveCount(
		20,
	);
	await showcase.getByRole("button", { name: "Next slide" }).click();
	await expect(
		showcase.getByRole("group", { name: "2 of 20", exact: true }),
	).not.toHaveAttribute("inert");

	// Turned off, Home is its rows.
	await page.goto("/settings/home");
	await page.getByRole("switch", { name: "Showcase" }).click();
	await page.goto("/");
	await expect(
		page.getByRole("region", { name: "Continue Watching" }),
	).toBeVisible();
	await expect(showcase).toHaveCount(0);
});

test("home's showcase says what the apps' does of an episode", async ({
	page,
}) => {
	// The first library row led by an episode that has everything the
	// showcase draws, whose show lends it what it lacks.
	await page.route(
		"**/api/v1/libraries/l-films/rows/recently_added_films*",
		async (route) => {
			const response = await route.fetch();
			const page = await response.json();
			const episode = {
				id: "t-ep",
				kind: "episode",
				title: "Pilot",
				added_at: "2024-03-12T00:00:00Z",
				show: { id: "t-show", title: "Small Show" },
				season_number: 1,
				episode_number: 1,
				release_date: "2024-03-12",
				certificate: "TV-14",
				overview: "A night shift at a radio station.",
				duration_ms: 1_800_000,
				state: { position_ms: 600_000 },
				ratings: [{ site: "imdb", score: 81 }],
				logo: "logo-svg",
			};
			await route.fulfill({
				response,
				json: { ...page, items: [episode, ...page.items] },
			});
		},
	);
	await page.route("**/api/v1/titles/t-show", async (route) => {
		const response = await route.fetch();
		const show = await response.json();
		await route.fulfill({
			response,
			json: {
				...show,
				genres: ["Drama", "Mystery"],
				ratings: [
					{ site: "imdb", score: 70 },
					{ site: "rotten_tomatoes", score: 91 },
				],
			},
		});
	});
	// A lettering kept as SVG on fanart.tv's clear 800 by 310 canvas, its ink
	// in the middle half.
	await page.route("**/api/v1/artwork/logo-svg*", (route) =>
		route.fulfill({
			contentType: "image/svg+xml",
			body: '<svg xmlns="http://www.w3.org/2000/svg" width="800" height="310" viewBox="0 0 800 310"><rect x="200" y="60" width="400" height="190" fill="#fc0"/></svg>',
		}),
	);
	await logIn(page);
	const showcase = page.getByRole("region", { name: "Showcase" });
	const logo = showcase.getByRole("img", { name: "Small Show" });
	await expect(logo).toBeVisible();
	// The ink alone is drawn, so it lines up with the words under it.
	const place = showcase.getByText("S1, E1 · Mar 12, 2024 · Drama · Mystery");
	const [ink, words] = await Promise.all([
		logo.boundingBox(),
		place.boundingBox(),
	]);
	expect(Math.abs((ink?.x ?? 0) - (words?.x ?? 0))).toBeLessThan(1);
	// Read from a 128-pixel sample, as the apps read it: within a pixel of it.
	const aspect = (ink?.width ?? 0) / (ink?.height ?? 1);
	expect(Math.abs(aspect / (400 / 190) - 1)).toBeLessThan(0.06);
	await expect(
		showcase.getByText("S1, E1 · Mar 12, 2024 · Drama · Mystery"),
	).toBeVisible();
	await expect(showcase.getByText("Pilot", { exact: true })).toBeVisible();
	await expect(
		showcase.getByText("A night shift at a radio station."),
	).toBeVisible();
	const facts = showcase.getByRole("list", { name: "Facts" });
	await expect(facts).toContainText("20m left");
	await expect(facts).toContainText("TV-14");
	// Its own IMDb score, and the show's Rotten Tomatoes where it has none.
	await expect(facts).toContainText("8.1");
	await expect(facts).toContainText("91%");
	await expect(facts).not.toContainText("7.0");
	await expect(
		showcase.getByRole("link", { name: "Resume 00:10:00 Small Show: Pilot" }),
	).toHaveAttribute("href", "/play/t-ep");
	await expectAccessible(page);
});

test("a title's page is centred on a phone, as the apps' is", async ({
	page,
}) => {
	await logIn(page, "/titles/t-film");
	const name = page.getByRole("heading", { level: 1, name: "Quiet Hours" });
	// The row of Play and what sits beside it.
	const actions = page
		.getByRole("link", { name: /^(Play|Resume)/ })
		.locator("..");
	const middle = async () => {
		const main = await page.getByRole("main").boundingBox();
		return (main?.x ?? 0) + (main?.width ?? 0) / 2;
	};
	const centre = async (el: typeof name) => {
		const box = await el.boundingBox();
		return (box?.x ?? 0) + (box?.width ?? 0) / 2;
	};
	// Wider, it keeps to the leading edge.
	await expect(name).toBeVisible();
	expect(Math.abs((await centre(name)) - (await middle()))).toBeGreaterThan(50);
	// An iPhone 16 Pro Max.
	await page.setViewportSize({ width: 440, height: 956 });
	await expect
		.poll(async () => Math.abs((await centre(name)) - (await middle())))
		.toBeLessThan(2);
	expect(Math.abs((await centre(actions)) - (await middle()))).toBeLessThan(2);
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

test("a wall drawn again at another width says nothing to the console", async ({
	page,
}) => {
	// The wall takes its height from the width it is told, so measuring during
	// the browser's own round of observers leaves that round undelivered.
	const shouted = () =>
		page.evaluate(() => (window as unknown as { shouted: string[] }).shouted);
	await page.addInitScript(() => {
		const heard: string[] = [];
		Object.assign(window, { shouted: heard });
		window.addEventListener("error", (e) => heard.push(e.message));
	});
	await logIn(page, "/libraries/l-films");
	const first = page
		.getByRole("list", { name: "Films" })
		.getByRole("listitem")
		.first();
	const wide = await first.boundingBox();
	await page.setViewportSize({ width: 900, height: 800 });
	await expect
		.poll(async () => (await first.boundingBox())?.width)
		.not.toBe(wide?.width);
	expect(await shouted()).toEqual([]);
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

test("a show library's wall shows its episodes across its shows", async ({
	page,
}) => {
	await logIn(page, "/libraries/l-shows");
	await expect(page.getByText("1 title", { exact: true })).toBeVisible();
	await page.getByRole("radio", { name: "Episodes" }).click();
	await expect(page).toHaveURL("/libraries/l-shows?kind=episodes");
	await expect(page.getByText("3 episodes", { exact: true })).toBeVisible();
	// A collection holds titles, so a narrowed wall of episodes is none.
	await expect(
		page.getByRole("button", { name: "Save as a smart collection" }),
	).toHaveCount(0);
	await page.getByRole("radio", { name: "Shows" }).click();
	await expect(page).toHaveURL("/libraries/l-shows");
	await expect(page.getByText("1 title", { exact: true })).toBeVisible();
});

test("a mixed library's wall shows its films, its shows, or both", async ({
	page,
}) => {
	await logIn(page, "/libraries/l-mixed");
	const all = page.getByText(/^\d+ titles$/);
	await expect(all).toBeVisible();
	const both = Number((await all.textContent())?.split(" ")[0]);
	await page.getByRole("radio", { name: "Shows" }).click();
	await expect(page).toHaveURL("/libraries/l-mixed?kind=shows");
	await expect(page.getByText("1 title", { exact: true })).toBeVisible();
	await page.getByRole("radio", { name: "Films" }).click();
	await expect(page).toHaveURL("/libraries/l-mixed?kind=films");
	await expect(
		page.getByText(`${both - 1} titles`, { exact: true }),
	).toBeVisible();
	await page.getByRole("radio", { name: "Episodes" }).click();
	await expect(page.getByText("3 episodes", { exact: true })).toBeVisible();
	await page.getByRole("radio", { name: "All" }).click();
	await expect(page).toHaveURL("/libraries/l-mixed");
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

test("the bar says what the server is doing: who is watching, a scan as it goes, the tasks running and a backlog counting down", async ({
	page,
}) => {
	await logIn(page, "/libraries/l-films");
	await page.getByRole("button", { name: /^Activity/ }).click();
	const menu = page.getByRole("dialog", { name: "Activity" });
	const streams = menu.getByRole("list", { name: "Streams" });
	await expect(streams).toContainText("Quiet Hours");
	await expect(streams).toContainText("Kids · Living Room");
	await expect(
		streams.getByRole("progressbar", {
			name: "How far Kids is through Quiet Hours",
		}),
	).toBeVisible();
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

test("a film's page plays the version and tracks chosen", async ({ page }) => {
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
	await page.getByRole("combobox", { name: "Audio" }).click();
	await page.getByRole("option", { name: /Commentary/ }).click();
	await expect(play).toHaveAttribute(
		"href",
		"/play/t-film?version=v-4k&audio=2",
	);
	await page.getByRole("combobox", { name: "Version" }).click();
	await page.getByRole("option", { name: /1080p/ }).click();
	await expect(play).toHaveAttribute("href", "/play/t-film?version=v-hd");
	await expect(page.getByRole("combobox", { name: "Audio" })).toHaveCount(0);

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
	// The 1080p version is in two files: each is a download of its own.
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

test("a show opens on itself, and its season on the episode the reader is at", async ({
	page,
}) => {
	await logIn(page, "/titles/t-show");
	await expect(page.getByRole("heading", { name: "Small Show" })).toBeVisible();
	await expect(
		page.getByRole("link", { name: "Resume S1, E1" }),
	).toHaveAttribute("href", "/play/t-ep");
	const seasons = page.getByRole("region", { name: /Seasons/ });
	await seasons.getByRole("link", { name: /Season 2/ }).click();
	await expect(page).toHaveURL("/titles/t-ep3");
	await page.getByRole("link", { name: "Small Show" }).click();
	await expect(page).toHaveURL("/titles/t-show");
	await expectAccessible(page);
});

test("an episode's page shows what its show is like", async ({ page }) => {
	await logIn(page, "/titles/t-ep");
	const like = page.getByRole("region", { name: /More like this/ });
	await expect(
		like.getByRole("link", { name: /More like this/ }),
	).toHaveAttribute("href", "/titles/t-show/similar");
});

test("an episode's season row picks another season's episodes", async ({
	page,
}) => {
	await logIn(page, "/titles/t-ep");
	const row = page.getByRole("region", { name: /Season 1/ });
	await expect(row.getByRole("link", { name: /Second/ })).toBeVisible();
	await row.getByRole("combobox", { name: "Season 1" }).click();
	await page.getByRole("option", { name: "Season 2" }).click();
	const picked = page.getByRole("region", { name: /Season 2/ });
	await expect(picked.getByRole("link", { name: /Return/ })).toBeVisible();
	await expect(picked.getByRole("link", { name: /Second/ })).toHaveCount(0);
	await expect(page).toHaveURL("/titles/t-ep");
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

test("the arrow keys step along an episode's season row", async ({ page }) => {
	await logIn(page, "/titles/t-ep");
	const row = page.getByRole("region", { name: /Season 1/ });
	const pilot = row.getByRole("link", { name: /Pilot/ });
	const second = row.getByRole("link", { name: /Second/ });
	await pilot.focus();
	await page.keyboard.press("ArrowRight");
	await expect(second).toBeFocused();
	await page.keyboard.press("ArrowRight");
	await expect(second).toBeFocused();
	await page.keyboard.press("ArrowLeft");
	await expect(pilot).toBeFocused();
	// From a card clicked, too: Safari focuses no link it is clicked on.
	await second.click();
	await expect(page).toHaveURL("/titles/t-ep2");
	await page.keyboard.press("ArrowLeft");
	await expect(pilot).toBeFocused();
});

test("picking another episode of the season keeps the page where it was", async ({
	page,
}) => {
	await page.setViewportSize({ width: 1200, height: 360 });
	await logIn(page, "/titles/t-ep");
	await expect(page.getByRole("heading", { name: "Season 1" })).toBeVisible();
	// The row in view, as the reader has it to pick from: a click scrolls to
	// what it clicks first.
	await page
		.getByRole("region", { name: /Season 1/ })
		.evaluate((row) => row.scrollIntoView({ block: "end" }));
	const before = await page.evaluate(() => window.scrollY);
	expect(before).toBeGreaterThan(0);
	await page.getByRole("link", { name: /Second/ }).click();
	await expect(page).toHaveURL("/titles/t-ep2");
	await expect(page.getByRole("link", { name: /Second/ })).toHaveAttribute(
		"aria-current",
		"page",
	);
	expect(await page.evaluate(() => window.scrollY)).toBe(before);
});

test("another title's page starts its rows at their start", async ({
	page,
}) => {
	await logIn(page, "/titles/t-film");
	const similar = page
		.getByRole("region", { name: /More like this/ })
		.getByRole("list");
	await expect(similar.getByRole("listitem")).toHaveCount(20);
	await similar.evaluate((row) =>
		row.scrollTo({ left: row.scrollWidth, behavior: "instant" }),
	);
	expect(await similar.evaluate((row) => row.scrollLeft)).toBeGreaterThan(0);
	// From the keyboard: a click could land on the card beside it as the row
	// settles.
	const last = similar.getByRole("listitem").last().getByRole("link").first();
	const href = await last.getAttribute("href");
	await last.focus();
	await page.keyboard.press("Enter");
	await expect(page).toHaveURL(href ?? "");
	await expect(
		similar.getByRole("link", { name: /Quiet Hours/ }),
	).toBeVisible();
	expect(await similar.evaluate((row) => row.scrollLeft)).toBe(0);
});

test("going back to another episode leaves the season and dialog picked on this one", async ({
	page,
}) => {
	await logIn(page, "/titles/t-ep");
	await page.getByRole("link", { name: /Second/ }).click();
	await expect(page).toHaveURL("/titles/t-ep2");
	await page
		.getByRole("region", { name: /Season 1/ })
		.getByRole("combobox", { name: "Season 1" })
		.click();
	await page.getByRole("option", { name: "Season 2" }).click();
	await expect(page.getByRole("region", { name: /Season 2/ })).toBeVisible();
	await page.getByRole("button", { name: "More", exact: true }).click();
	await page.getByRole("menuitem", { name: "Media info" }).click();
	await expect(page.getByRole("dialog", { name: "Media info" })).toBeVisible();
	await page.goBack();
	await expect(page).toHaveURL("/titles/t-ep");
	await expect(page.getByRole("dialog")).toHaveCount(0);
	await expect(
		page.getByRole("region", { name: /Season 1/ }).getByRole("link", {
			name: /Pilot/,
		}),
	).toHaveAttribute("aria-current", "page");
});

test("a show's theme goes on from one of its episodes to the next", async ({
	page,
}) => {
	await logIn(page, "/settings/playback");
	await page.getByRole("switch", { name: "Play theme music" }).click();
	await expect(page.getByText("Saved for every device.")).toBeVisible();
	await page.goto("/titles/t-ep");
	const tune = await page.locator("audio[data-theme-tune]").elementHandle();
	await page.getByRole("link", { name: /Second/ }).click();
	await expect(page.getByRole("link", { name: /Second/ })).toHaveAttribute(
		"aria-current",
		"page",
	);
	expect(await tune?.evaluate((audio) => audio.isConnected)).toBe(true);
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
	// An episode is found by its season's poster, as its still says nothing of
	// which show it is of.
	await page.goto("/search?q=pilot");
	const found = page.getByRole("region", { name: "Episodes" });
	await expect(found.locator("img").first()).toHaveAttribute(
		"src",
		/\/api\/v1\/artwork\/1c5f3a2b-8d7e-4f6a-b5c4-d3e2f1a0b9c8/,
	);
	// A run of results narrowed to a library leads back to them so narrowed.
	await page.goto("/search/movie?q=quiet&library=l-films");
	await expect(page.getByRole("link", { name: /Results for/ })).toHaveAttribute(
		"href",
		"/search?q=quiet&library=l-films",
	);
});

test("a show shuffles into one of its episodes, from its page or an episode's", async ({
	page,
}) => {
	await logIn(page, "/titles/t-show");
	await page.getByRole("button", { name: "Shuffle" }).click();
	await expect(page).toHaveURL(/\/play\/t-ep[23]?$/);
	await page.goto("/titles/t-ep2");
	await page.getByRole("button", { name: "Shuffle" }).click();
	await expect(page).toHaveURL(/\/play\/t-ep[23]?$/);
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
	await page.getByRole("button", { name: "Move Small Show: Pilot up" }).click();
	await expect(
		page.getByRole("button", { name: "Move Small Show: Pilot up" }),
	).toBeDisabled();
	await expectAccessible(page);
	await page.getByRole("button", { name: "Play all" }).click();
	await expect(page).toHaveURL("/play/t-ep");
	await page.goBack();

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

test("a filtered wall is kept as a smart playlist, and its filters changed there", async ({
	page,
}) => {
	await logIn(page, "/libraries/l-shows?kind=episodes&mark=unwatched");
	const made = page.waitForRequest(
		(r) => r.method() === "POST" && r.url().endsWith("/api/v1/playlists"),
	);
	await page.getByRole("button", { name: "Save as a smart playlist" }).click();
	await page.getByLabel("Name").fill("Not yet seen");
	await page.getByRole("button", { name: "Save", exact: true }).click();
	expect((await made).postDataJSON()).toMatchObject({
		name: "Not yet seen",
		library_id: "l-shows",
		rule: { kind: "episodes", filter: { marks: ["unwatched"] } },
	});
	await expect(page).toHaveURL(/\/playlists\/pl-\d+$/);
	const list = page.url().split("/").pop();
	await expect(page.getByText(/Smart playlist/)).toBeVisible();
	await expect(page.getByText("3 titles")).toBeVisible();
	await expectAccessible(page);
	// Its entries are its rule's findings, so none of them is moved or taken out.
	await expect(page.getByRole("button", { name: /^Move / })).toHaveCount(0);
	await expect(page.getByRole("button", { name: /^Remove / })).toHaveCount(0);

	await page.getByRole("link", { name: "Change the filters" }).click();
	await expect(page).toHaveURL(
		`/libraries/l-shows?kind=episodes&mark=unwatched&playlist=${list}`,
	);
	const saved = page.waitForRequest(
		(r) => r.method() === "PUT" && r.url().endsWith(`/${list}/rule`),
	);
	await page.getByRole("button", { name: "Save to the playlist" }).click();
	expect((await saved).postDataJSON()).toMatchObject({
		kind: "episodes",
		filter: { marks: ["unwatched"] },
	});
	await expect(page).toHaveURL(`/playlists/${list}`);

	await page.getByRole("button", { name: "Delete" }).click();
	await page
		.getByRole("alertdialog")
		.getByRole("button", { name: "Delete" })
		.click();
	await expect(page.getByText("No playlists yet.")).toBeVisible();
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

test("a favourite episode is a poster, as the apps draw it", async ({
	page,
}) => {
	await page.route("**/api/v1/profile/rows/favourites*", async (route) => {
		const response = await route.fetch();
		const row = await response.json();
		const episode = {
			id: "t-ep",
			kind: "episode",
			title: "Pilot",
			added_at: "2024-03-12T00:00:00Z",
			show: { id: "t-show", title: "Small Show" },
			season_number: 1,
			episode_number: 1,
			thumb: "still",
			season_poster: "season-poster",
		};
		await route.fulfill({
			response,
			json: { ...row, items: [...(row.items ?? []), episode] },
		});
	});
	await logIn(page, "/favourites");
	const episodes = page.getByRole("region", { name: "Episodes" });
	const pilot = episodes.getByRole("link", { name: /Pilot/ });
	await expect(pilot).toContainText("S1, E1 · Small Show");
	await expect(pilot.locator(".aspect-2\\/3")).toHaveCount(1);
	await expect(pilot.locator("img")).toHaveAttribute(
		"src",
		/artwork\/season-poster/,
	);
});

test("a version in two files is downloaded a file at a time", async ({
	page,
}) => {
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

test("an episode's card leads to its season, which opens on an episode", async ({
	page,
}) => {
	await logIn(page, "/titles/t-ep");
	await page.getByRole("button", { name: "Favourite" }).click();
	await page.goto("/");
	await page
		.getByRole("region", { name: "Favourites" })
		.getByRole("button", { name: "More for Small Show: Pilot" })
		.click();
	await page.getByRole("menuitem", { name: "Go to Season 1" }).click();
	await expect(page).toHaveURL("/titles/t-ep");
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

test("a title's one version goes to its download without a choice", async ({
	page,
}) => {
	let answer = () => {};
	const answered = new Promise<void>((done) => {
		answer = done;
	});
	await page.route("**/api/v1/titles/t-film", async (route) => {
		const response = await route.fetch();
		const title = await response.json();
		await answered;
		await route.fulfill({
			response,
			json: { ...title, versions: title.versions.slice(0, 1) },
		});
	});
	await logIn(page);
	await page
		.getByRole("region", { name: "Recently Added in Films" })
		.getByRole("button", { name: "More for Quiet Hours" })
		.click();
	await page.getByRole("menuitem", { name: "Download…" }).click();
	await expect(page.getByRole("menu")).toBeHidden();
	await expect(page.getByRole("dialog")).toBeHidden();
	answer();
	await expect(page.getByRole("dialog", { name: "Download" })).toBeVisible();
	await expect(
		page.getByRole("dialog", { name: "Download version" }),
	).toBeHidden();
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

test("an episode's page names its show by the show's lettering alone", async ({
	page,
}) => {
	await logIn(page, "/titles/t-ep");
	const heading = page.getByRole("heading", { level: 1 });
	await expect(heading.getByRole("img", { name: "Small Show" })).toBeVisible();
	await expect(
		page
			.getByRole("main")
			.locator("header")
			.getByText("Small Show", { exact: true }),
	).toHaveCount(0);
	await expect(heading.getByRole("link")).toHaveAttribute(
		"href",
		"/titles/t-show",
	);
});

test("collections, in no library, have a view of their own in the sidebar", async ({
	page,
}) => {
	await logIn(page, "/");
	await page
		.getByRole("navigation")
		.getByRole("link", { name: "Collections", exact: true })
		.click();
	await expect(page).toHaveURL("/collections");
	await expect(
		page.getByRole("heading", { name: "Collections", level: 1 }),
	).toBeVisible();
	await expect(
		page.getByRole("link", { name: /Lighthouse Films/ }),
	).toBeVisible();
});
