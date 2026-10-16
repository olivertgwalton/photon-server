// Takes the screenshots the docs show (docs/images/*.webp): most from a real
// server, signed in once beforehand, and what a real server should not be made
// to show for a picture (a new server, a play under way, a key just made) from
// the e2e mock. Nothing here saves a change on the real server: it opens pages,
// menus and dialogs, and types into fields it never submits.
//
//   bun x playwright open --save-storage=session.json <server>/auth/login
//   DOCS_SERVER=<server> DOCS_SESSION=session.json bun scripts/docs-screenshots.ts [name…]
//
// The mock is built and served on DOCS_MOCK_PORT (4310) for the mock's shots.
import { execFileSync, spawn } from "node:child_process";
import { mkdirSync, rmSync } from "node:fs";
import path from "node:path";
import { type Browser, chromium, type Locator, type Page } from "playwright";

const server = process.env.DOCS_SERVER ?? "http://localhost:8640";
const session = process.env.DOCS_SESSION;
const mockPort = Number(process.env.DOCS_MOCK_PORT ?? 4310);
const images = path.resolve(import.meta.dirname, "../../docs/images");
const scratch = path.join(images, ".png");

type Ids = Record<string, string>;
type Shot = {
	file: string;
	// The page to open, given the real server's ids.
	at: string | ((ids: Ids) => string);
	// What to do to the page before the picture is taken.
	act?: (page: Page) => Promise<unknown>;
	// Only this part of the page, where the shot is of one thing on it.
	of?: (page: Page) => Locator;
	// What to arrange before the page opens, such as a response laid over.
	before?: (page: Page) => Promise<unknown>;
	// A part of the viewport, in CSS pixels.
	clip?: { x: number; y: number; width: number; height: number };
	mock?: true;
};

const button = (page: Page, name: string | RegExp) =>
	page.getByRole("button", { name }).first();
const scrollTo = (page: Page, text: string | RegExp) =>
	page.getByText(text).first().scrollIntoViewIfNeeded();

const card = (page: Page, title: string) =>
	page.getByRole("button", { name: new RegExp(`^More for ${title}`) }).first();
const dialog = (page: Page) => page.getByRole("dialog").last();
const newServer = (page: Page) =>
	page.route("**/api/v1/setup", (route) =>
		route.request().method() === "GET"
			? route.fulfill({ json: { state: "open" } })
			: route.fallback(),
	);
// A play on the real server reports nothing back, so no resume point moves.
const quietly = (page: Page) =>
	page.route(/\/api\/v1\/playbacks\/[^/]+\/(progress|stop)$/, (route) =>
		route.fulfill({ status: 204 }),
	);
// The player's controls, over black: no frame of anyone's library is shown.
const playing = async (page: Page) => {
	await page.waitForTimeout(1500);
	await page.addStyleTag({
		content: "video, canvas { visibility: hidden !important; }",
	});
	await page.mouse.move(720, 450);
	await page.mouse.move(730, 460);
};

export const shots: Shot[] = [
	// Get started
	{ file: "get-started-log-in", at: "/auth/login", mock: true },
	{ file: "get-started-title-play", at: (i) => `/titles/${i.film}` },
	{
		file: "get-started-link-a-device",
		at: "/settings/link",
		act: (p) => p.getByLabel("Code").fill("BCDF-GHJK"),
	},
	{
		file: "get-started-create-admin",
		at: "/auth/login",
		mock: true,
		act: async (p) => {
			await p.context().clearCookies();
			await newServer(p);
			await p.goto("/setup");
			await p.getByLabel("Name").fill("Ada");
			await p.getByLabel("Password", { exact: true }).fill("correct horse");
			await p.getByLabel("Confirm password").fill("correct horse");
		},
	},
	{
		file: "get-started-add-libraries",
		at: "/setup/libraries",
		mock: true,
		act: async (p) => {
			await p.getByLabel("Name").fill("Shows");
			await p.getByRole("button", { name: "Holds" }).click();
			await p.getByRole("option", { name: "Shows", exact: true }).click();
			await p.getByRole("button", { name: "Browse…" }).click();
			await dialog(p).getByRole("button", { name: "media" }).click();
		},
	},
	{ file: "get-started-setup-done", at: "/setup/done", mock: true },

	// Web app
	{ file: "web-app-login", at: "/auth/login", mock: true },
	{ file: "web-app-home", at: "/" },
	{
		file: "web-app-card-menu",
		at: "/",
		act: (p) => card(p, "Kitchen Nightmares").click(),
	},
	{ file: "web-app-library", at: (i) => `/libraries/${i.films}` },
	{
		file: "web-app-library-filter",
		at: (i) => `/libraries/${i.films}`,
		act: (p) => button(p, "Filter").click(),
	},
	{ file: "web-app-title", at: (i) => `/titles/${i.film}` },
	{ file: "web-app-search", at: "/search?q=dark" },
	{ file: "web-app-calendar", at: "/calendar" },
	{
		file: "web-app-playlist",
		at: "/",
		mock: true,
		act: async (p) => {
			const made = await (
				await p.request.post("/api/v1/playlists", {
					data: { name: "Friday night" },
				})
			).json();
			await p.request.post(`/api/v1/playlists/${made.id}/entries`, {
				data: { item_ids: ["t-film", "t-ep", "t-ep2"] },
			});
			await p.goto(`/playlists/${made.id}`);
		},
	},
	{
		file: "web-app-player-controls",
		at: (i) => `/play/${i.episode}`,
		before: quietly,
		act: playing,
	},
	{
		file: "web-app-player-settings",
		at: (i) => `/play/${i.episode}`,
		before: quietly,
		act: async (p) => {
			await playing(p);
			await p.getByRole("button", { name: "Settings" }).click();
		},
	},
	{
		file: "web-app-player-episodes",
		at: (i) => `/play/${i.episode}`,
		before: quietly,
		act: async (p) => {
			await playing(p);
			await p.getByRole("button", { name: "Episodes" }).click();
		},
	},
	{ file: "web-app-settings-playback", at: "/settings/playback" },
	{
		file: "web-app-subtitle-style",
		at: "/settings/playback",
		act: (p) => scrollTo(p, "Subtitles on this device"),
	},

	// Library
	{ file: "library-libraries-list", at: "/settings/server/libraries" },
	{
		file: "library-add-library",
		at: "/settings/server/libraries/new",
		act: async (p) => {
			await p.getByRole("textbox", { name: "Name" }).fill("Films");
			await p.getByRole("textbox", { name: "Folder" }).fill("/media/Films");
		},
	},
	{
		file: "library-settings",
		at: (i) => `/settings/server/libraries/${i.films}`,
		act: (p) => scrollTo(p, /Metadata downloaders/),
	},
	{
		file: "library-sources-ranking",
		at: (i) => `/settings/server/libraries/${i.films}`,
		act: (p) => scrollTo(p, /Metadata downloaders/),
	},
	{ file: "library-metadata-providers", at: "/settings/server/providers" },
	{
		file: "library-title-menu",
		at: (i) => `/libraries/${i.films}`,
		act: (p) => card(p, "1917").click(),
	},
	{
		file: "library-fix-match-dialog",
		at: (i) => `/libraries/${i.films}`,
		act: async (p) => {
			await card(p, "1917").click();
			await p.getByRole("menuitem", { name: "Fix match…" }).click();
			await dialog(p).getByRole("button", { name: "Search" }).click();
		},
	},
	{
		file: "library-manage-title",
		at: (i) => `/settings/server/titles/${i.film}`,
	},
	{
		file: "library-edit-details",
		at: (i) => `/libraries/${i.films}`,
		act: async (p) => {
			await card(p, "1917").click();
			await p.getByRole("menuitem", { name: "Edit…" }).click();
		},
	},
	{
		file: "library-scheduled-tasks",
		at: "/settings/server/tasks",
		act: (p) => scrollTo(p, "Scan libraries"),
	},
	{ file: "library-collections-list", at: "/settings/server/collections" },
	{
		file: "library-collection-by-hand",
		at: (i) => `/settings/server/collections/${i.collection}`,
	},
	{
		file: "library-smart-collection",
		at: (i) => `/libraries/${i.films}`,
		act: async (p) => {
			await button(p, "Filter").click();
			await p.getByRole("menuitem", { name: /Genre/ }).first().hover();
			await p.getByRole("menuitemcheckbox", { name: "Drama" }).first().click();
			await p.keyboard.press("Escape");
			await button(p, /Save as a smart collection/).click();
			await dialog(p).getByRole("textbox").first().fill("Dramas");
		},
	},
	{
		file: "library-collection-from-list",
		at: "/settings/server/collections",
		act: async (p) => {
			await p
				.getByRole("textbox", { name: "New collection" })
				.fill("Top rated");
			await p.getByRole("button", { name: "Titles" }).click();
			await p.getByRole("option", { name: /TMDB list/ }).click();
		},
	},
	{
		file: "library-browse-lists",
		at: "/settings/server/collections",
		act: (p) => button(p, "Browse lists").click(),
	},
	{ file: "library-home", at: "/" },
	{ file: "library-settings-home", at: "/settings/home" },
	{
		file: "library-timeline-preview",
		at: (i) => `/play/${i.episode}`,
		before: quietly,
		act: async (p) => {
			await playing(p);
			const bar = p.getByRole("slider").first();
			const box = await bar.boundingBox();
			if (box)
				await p.mouse.move(box.x + box.width * 0.4, box.y + box.height / 2);
		},
	},
	{
		file: "library-markers-settings",
		at: (i) => `/settings/server/libraries/${i.shows}`,
		act: (p) => scrollTo(p, /^Previews$/),
	},
	{
		file: "library-edit-markers",
		at: (i) => `/settings/server/titles/${i.episode}#markers`,
		act: (p) =>
			p
				.getByRole("heading", { name: /^Markers/ })
				.first()
				.scrollIntoViewIfNeeded(),
	},
	{
		file: "library-skip-settings",
		at: "/settings/playback",
		act: (p) => scrollTo(p, /Intros and recaps/),
	},
	{ file: "library-maintenance-window", at: "/settings/server/tasks" },
	{
		file: "library-opensubtitles",
		at: "/settings/server/providers",
		act: (p) => scrollTo(p, "OpenSubtitles"),
	},
	{
		file: "library-subtitles-to-download",
		at: (i) => `/settings/server/libraries/${i.films}`,
		act: (p) => scrollTo(p, /Subtitles to download/),
	},
	{
		file: "library-play-theme-music",
		at: "/settings/playback",
		act: (p) => scrollTo(p, "Play theme music"),
	},
	{
		file: "library-theme-music-setting",
		at: (i) => `/settings/server/libraries/${i.shows}`,
		act: (p) => scrollTo(p, /^Theme music$/),
	},

	// Access
	{ file: "access-profiles-list", at: "/settings/profiles", mock: true },
	{
		file: "access-profile-what-it-sees",
		at: "/settings/profiles/p-kids",
		mock: true,
		act: (p) => scrollTo(p, "What it sees"),
	},
	{
		file: "access-sign-in-add-provider",
		at: "/settings/server/sign-in",
		// The page offers a provider only once the server has a public address;
		// an example one is laid over what it answers, and nothing is saved.
		before: (p) =>
			p.route("**/api/v1/admin/network", async (route) => {
				const real = await (await route.fetch()).json();
				await route.fulfill({
					json: { ...real, public_url: "https://photon.example.com" },
				});
			}),
		act: async (p) => {
			await button(p, /Add a provider/).click();
			await p.getByLabel("Slug").fill("pocket-id");
			await p.getByLabel("Name", { exact: true }).fill("Pocket ID");
		},
	},
	{
		file: "access-link-a-device",
		at: "/settings/link",
		act: (p) => p.getByLabel("Code").fill("BCDF-GHJK"),
	},
	{ file: "access-network", at: "/settings/server/network" },
	{
		file: "access-jellyfin-apps",
		at: "/settings/server/network",
		act: (p) => scrollTo(p, /Jellyfin apps/),
	},
	{
		file: "access-api-key-made",
		at: "/settings/server/keys",
		mock: true,
		act: async (p) => {
			await button(p, "Make a key").click();
			await dialog(p)
				.getByRole("textbox", { name: "What it is for" })
				.fill("Sonarr");
			await dialog(p).getByRole("button", { name: "Make key" }).click();
		},
	},
	{ file: "access-devices", at: "/settings/devices", mock: true },
	{ file: "access-now-playing", at: "/settings/server", mock: true },
	{ file: "access-activity", at: "/settings/server/activity" },

	// Integrations
	{
		file: "integrations-plugins",
		at: "/settings/server/providers",
		act: (p) => scrollTo(p, /^Plugins$/),
	},
	{
		file: "integrations-plugin-in-library",
		at: "/settings/server/libraries/l-films",
		mock: true,
		act: (p) => scrollTo(p, /Metadata downloaders/),
	},
	{
		file: "integrations-stremio-register",
		at: "/settings/server/providers",
		act: async (p) => {
			await scrollTo(p, /^Plugins$/);
			await p.getByRole("button", { name: "It speaks" }).click();
			await p.getByRole("option", { name: "Stremio addon" }).click();
			await p
				.getByRole("textbox", { name: "Register a plugin" })
				.fill("https://addon.example.com/manifest.json");
		},
	},
	{ file: "integrations-trackers-server", at: "/settings/server/trackers" },
	{
		file: "integrations-trackers-link",
		at: "/settings/trackers",
		mock: true,
		act: (p) => button(p, /Link Trakt/).click(),
	},
	{
		file: "integrations-webhook-add",
		at: "/settings/server/webhooks",
		act: async (p) => {
			await button(p, /Add a webhook/).click();
			await p.getByLabel(/Address/).fill("https://discord.com/api/webhooks/…");
		},
	},
	// Cut above the browser's own notice, which a headless browser always shows.
	{
		file: "integrations-notifications-settings",
		at: "/settings/notifications",
		clip: { x: 0, y: 0, width: 1440, height: 520 },
	},
	{
		file: "integrations-notifications-bell",
		at: "/",
		mock: true,
		act: (p) => button(p, /Notifications/).click(),
	},
	{
		file: "integrations-import",
		at: "/settings/server/imports",
		act: async (p) => {
			await button(p, "Import").click();
			await dialog(p).getByRole("button", { name: "From" }).click();
			await p.getByRole("option", { name: "Jellyfin" }).click();
			await dialog(p)
				.getByRole("textbox", { name: "Address" })
				.fill("http://192.168.1.20:8096");
			// The admin's own profile, not another of the household's.
			await dialog(p).getByRole("button", { name: "Into" }).click();
			await p
				.getByRole("option")
				.filter({ hasText: process.env.DOCS_PROFILE ?? "" })
				.first()
				.click();
		},
	},

	// Playback
	{ file: "playback-now-playing", at: "/settings/server", mock: true },
	{
		file: "playback-because-of",
		at: "/settings/server",
		mock: true,
		of: (p) => p.getByRole("region", { name: "Now playing" }),
	},
	{
		file: "playback-hdr-tone-mapped",
		at: "/settings/server",
		mock: true,
		of: (p) => p.getByRole("region", { name: "Now playing" }),
	},
	{
		file: "playback-encoder-setting",
		at: "/settings/server",
		act: async (p) => {
			await p
				.getByRole("button", { name: /^Actions for/ })
				.first()
				.click();
			await p.getByRole("menuitem", { name: "Settings…" }).click();
		},
	},
	{
		file: "playback-download-dialog",
		at: (i) => `/titles/${i.film}`,
		act: async (p) => {
			await p
				.locator("main")
				.getByRole("button", { name: "More", exact: true })
				.click();
			await p.getByRole("menuitem", { name: "Download…" }).click();
		},
	},
	{
		file: "playback-downloads-page",
		at: "/downloads",
		mock: true,
		act: async (p) => {
			for (const id of ["t-film", "t-ep"]) {
				await p.request.post("/api/v1/downloads", {
					data: {
						title_id: id,
						max_bitrate_kbps: id === "t-film" ? 8000 : 1_000_000,
					},
				});
			}
			await p.reload();
		},
	},
	{
		file: "playback-nodes",
		at: "/settings/server",
		mock: true,
		act: (p) => scrollTo(p, /Nodes/),
	},
	{
		file: "playback-node-settings",
		at: "/settings/server",
		act: async (p) => {
			await p
				.getByRole("button", { name: /^Actions for/ })
				.first()
				.click();
			await p.getByRole("menuitem", { name: "Settings…" }).click();
		},
	},

	// Maintain
	{ file: "maintain-dashboard", at: "/settings/server" },
	{
		file: "maintain-version",
		at: "/settings/server",
		clip: { x: 520, y: 50, width: 920, height: 80 },
	},
	{ file: "maintain-backups", at: "/settings/server/backups" },
	{
		file: "maintain-restore-confirm",
		at: "/settings/server/backups",
		act: (p) => button(p, "Restore").click(),
	},
	{ file: "maintain-metrics", at: "/settings/server/metrics" },
	{ file: "maintain-tasks", at: "/settings/server/tasks" },
	{ file: "maintain-jobs", at: "/settings/server/jobs" },
	{ file: "maintain-storage", at: "/settings/server/storage" },
	{
		file: "maintain-storage-bucket",
		at: "/settings/server/storage",
		act: async (p) => {
			await p.getByRole("button", { name: "Kept on" }).click();
			await p.getByRole("option", { name: /bucket/ }).click();
		},
	},

	// Reference
	{
		file: "reference-check-access",
		at: "/settings/server/libraries",
		act: (p) => button(p, "Check access").click(),
	},
];

// The ids a real server's shots open, found by asking it.
async function idsOf(page: Page): Promise<Ids> {
	const get = async (p: string) =>
		(await page.request.get(`${server}/api/v1${p}`)).json();
	const libraries: { id: string; kind: string }[] = (await get("/libraries"))
		.items;
	const films = libraries.find((l) => l.kind === "movies")?.id ?? "";
	const shows = libraries.find((l) => l.kind === "shows")?.id ?? "";
	const film =
		process.env.DOCS_FILM ??
		(await get(`/libraries/${films}/titles?limit=1`)).items[0].id;
	const episode =
		process.env.DOCS_EPISODE ??
		(await get("/profile/rows/continue_watching?limit=1")).items[0].id;
	const collection = (await get("/collections")).items[0]?.id ?? "";
	return { films, shows, film, episode, collection };
}

async function settle(page: Page) {
	await page.waitForLoadState("networkidle").catch(() => {});
	await page.waitForFunction(() => document.title !== "");
	await page.evaluate(() => document.fonts.ready);
	// Pictures fade in; a shot taken mid-fade is half a picture.
	await page.waitForTimeout(900);
}

async function take(
	browser: Browser,
	shot: Shot,
	base: string,
	ids: Ids,
	signIn?: (p: Page) => Promise<void>,
) {
	const context = await browser.newContext({
		baseURL: base,
		storageState: shot.mock ? undefined : session,
		viewport: { width: 1440, height: 900 },
		deviceScaleFactor: 2,
		colorScheme: "dark",
		permissions: ["notifications"],
	});
	const page = await context.newPage();
	try {
		if (signIn && !shot.at.toString().startsWith("/auth")) await signIn(page);
		await shot.before?.(page);
		await page.goto(typeof shot.at === "string" ? shot.at : shot.at(ids));
		await settle(page);
		await shot.act?.(page);
		await settle(page);
		const png = path.join(scratch, `${shot.file}.png`);
		await (shot.of
			? shot.of(page).screenshot({ path: png })
			: page.screenshot({ path: png, clip: shot.clip }));
		execFileSync("cwebp", [
			"-quiet",
			"-q",
			"82",
			png,
			"-o",
			path.join(images, `${shot.file}.webp`),
		]);
		console.log(`✓ ${shot.file}`);
	} catch (error) {
		console.error(`✗ ${shot.file}: ${(error as Error).message.split("\n")[0]}`);
	} finally {
		await context.close();
	}
}

async function mockServer() {
	const web = path.resolve(import.meta.dirname, "..");
	execFileSync("bun", ["run", "build"], { cwd: web, stdio: "ignore" });
	const child = spawn("bun", ["e2e/mock-api.ts"], {
		cwd: web,
		env: { ...process.env, PORT: String(mockPort) },
	});
	for (let i = 0; i < 50; i++) {
		if (
			await fetch(`http://localhost:${mockPort}/api/v1/server`).then(
				(r) => r.ok,
				() => false,
			)
		)
			break;
		await new Promise((r) => setTimeout(r, 200));
	}
	return child;
}

async function mockSignIn(page: Page) {
	await page.goto("/auth/login");
	await page.getByLabel("Name").fill("Ada");
	await page.getByLabel("Password").fill("correct horse");
	await page.getByRole("button", { name: "Log in" }).click();
	await page.getByRole("button", { name: "Ada" }).click();
}

const only = process.argv.slice(2);
const chosen = shots.filter((s) => only.length === 0 || only.includes(s.file));
mkdirSync(scratch, { recursive: true });
const browser = await chromium.launch();
const ids = await (async () => {
	const context = await browser.newContext({ storageState: session });
	const found = await idsOf(await context.newPage());
	await context.close();
	return found;
})();
for (const shot of chosen.filter((s) => !s.mock))
	await take(browser, shot, server, ids);
const mocked = chosen.filter((s) => s.mock);
if (mocked.length) {
	const mock = await mockServer();
	for (const shot of mocked)
		await take(browser, shot, `http://localhost:${mockPort}`, ids, mockSignIn);
	mock.kill();
}
await browser.close();
rmSync(scratch, { recursive: true, force: true });
