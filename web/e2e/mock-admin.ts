// The admin half of the mock API: what a dashboard is shown of a small
// household's server, typed by the schema as mock-api.ts is.
import type { components } from "../src/lib/api/schema.d.ts";

type Schemas = components["schemas"];

const json = (body: unknown, status = 200) => Response.json(body, { status });
const done = (status = 204) => new Response(null, { status });

const playing: Schemas["NowPlaying"] = {
	id: "pb-1",
	profile: { id: "p-kids", name: "Kids" },
	device: {
		id: "d-tv",
		name: "Living Room",
		client: "Photon for tvOS",
		address: "10.0.0.20",
	},
	title: {
		id: "t-quiet",
		kind: "movie",
		title: "Quiet Hours",
		year: 2018,
	},
	version: {
		id: "v-quiet",
		container: "matroska",
		duration_ms: 6_000_000,
		bitrate_kbps: 18_000,
	},
	method: "transcode",
	reasons: ["video_codec_not_supported"],
	acceleration: "videotoolbox",
	video: {
		stream: 0,
		codec: "hevc",
		width: 3840,
		height: 2160,
		range: "hdr10",
		encode: { codec: "h264", width: 1920, height: 1080, tone_mapped: true },
	},
	audio: { stream: 1, codec: "eac3", channels: 6, language: "en" },
	state: "playing",
	position_ms: 1_200_000,
	started_at: "2026-10-06T20:00:00Z",
	updated_at: "2026-10-06T20:20:00Z",
	node_id: "n-1",
};

const server: Schemas["Server"] = {
	id: "s-1",
	name: "Den",
	version: "v1.0.0",
	node_id: "n-1",
	started_at: "2026-10-06T08:00:00Z",
	os: "linux",
	arch: "arm64",
	ffmpeg: { path: "/usr/bin/ffmpeg", version: "9.0" },
	ffprobe: { path: "/usr/bin/ffprobe", version: "9.0" },
	yt_dlp: { path: "", version: "" },
	chromaprint: true,
	encoder: {
		acceleration: "vaapi",
		device: "/dev/dri/renderD128",
		hevc: "allow",
	},
	transcode_limit: 2,
	discovery: "broadcast",
	listen: ":8640",
	trusted_proxies: [],
	folders: {
		cache: { path: "/cache", free_bytes: 120_000_000_000 },
		backups: { path: "/backups", free_bytes: 120_000_000_000 },
	},
	metadata_language: "en-GB",
	postgres: { reachable: true, version: "18.1" },
	valkey: { reachable: true, version: "9.0" },
	nodes: [],
};

const films: Schemas["AdminLibrary"] = {
	id: "l-films",
	name: "Films",
	kind: "movies",
	root: "/media/films",
	sources: [
		{
			kind: "movie",
			metadata: [
				{ source: "nfo", enabled: true },
				{ source: "tmdb", enabled: true },
			],
			images: [{ source: "tmdb", enabled: true }],
		},
	],
	remote_extras: ["trailer"],
	monitor: "realtime",
	refresh_days: 30,
	previews: "all",
	markers: "all",
	keyframes: "index",
	themes: "local",
	deletion: "off",
	artwork_language: "localized",
	title_language: "localized",
	collection_mode: "grouped",
};

const providers: Schemas["MetadataProvider"][] = [
	{
		id: "tmdb",
		name: "TMDB",
		kinds: ["movie", "show"],
		capabilities: ["describe", "search", "person"],
		settings: [],
		ready: true,
		metadata_kinds: ["movie", "show", "season", "episode"],
		image_kinds: ["movie", "show", "season", "episode"],
	},
	{
		id: "tvdb",
		name: "TheTVDB",
		kinds: ["show"],
		capabilities: ["describe", "search"],
		settings: [],
		ready: true,
		metadata_kinds: ["show", "season", "episode"],
		image_kinds: ["show", "season", "episode"],
	},
	{
		id: "mdblist",
		name: "MDBList",
		kinds: ["movie", "show"],
		capabilities: ["rate"],
		settings: [
			{
				key: "api_key",
				name: "API key",
				secret: true,
				required: true,
				set: false,
			},
		],
		ready: false,
		metadata_kinds: ["movie", "show"],
		image_kinds: [],
	},
];

const activity: Schemas["Event"][] = [
	{
		id: "e-2",
		kind: "task.failed",
		at: "2026-10-06T19:00:00Z",
		details: { task: "backup_database", error: "disk full" },
	},
	{
		id: "e-1",
		kind: "library.added",
		at: "2026-10-06T18:00:00Z",
		library_id: "l-films",
		details: { name: "Films" },
	},
];

const film: Schemas["TitlePage"] = {
	id: "t-quiet",
	kind: "movie",
	title: "Quiet Hours",
	year: 2018,
	added_at: "2026-10-01T20:00:00Z",
	overview: "Two nights in a lighthouse.",
	genres: ["Drama"],
	ids: { tmdb: "101" },
	versions: [
		{
			id: "v-quiet",
			container: "matroska",
			duration_ms: 6_000_000,
			size_bytes: 4_000_000_000,
			parts: 1,
			files: [
				{
					id: "p-quiet",
					index: 0,
					size_bytes: 4_000_000_000,
					duration_ms: 6_000_000,
					offset_ms: 0,
				},
			],
			streams: [],
			markers: [
				{
					kind: "credits",
					start_ms: 5_700_000,
					end_ms: 6_000_000,
					source: "chapter",
				},
			],
		},
	],
};

const collection: Schemas["TitlePage"] = {
	id: "t-box",
	kind: "collection",
	title: "Lighthouse Films",
	added_at: "2026-10-01T20:00:00Z",
	origin: "user",
};

let webhooks: Schemas["Webhook"][] = [];
let keys: Schemas["KeyListing"][] = [];
let chosenPoster = "a-1";
let maintenance: Schemas["Maintenance"] = {
	start_hour: 2,
	end_hour: 5,
	time_zone: "UTC",
	previews: "window",
	markers: "window_and_added",
};
let network: Schemas["NetworkStatus"] = {
	secure_connections: "disabled",
	jellyfin: "off",
	jellyfin_port: 8096,
};
const deadJobs: Schemas["DeadJob"][] = [
	{
		id: 41,
		kind: "identify",
		subject: "t-quiet",
		attempts: 5,
		error: "TMDB said 503",
	},
];

// The event stream: what is going on, then a scan finding more folders and the
// previews' backlog counting down.
function events() {
	const encoder = new TextEncoder();
	let timer: ReturnType<typeof setInterval> | undefined;
	const body = new ReadableStream({
		start(controller) {
			const send = (name: string, data: unknown) =>
				controller.enqueue(
					encoder.encode(`event: ${name}\ndata: ${JSON.stringify(data)}\n\n`),
				);
			send("snapshot", {
				tasks: [
					{
						key: "scan_libraries",
						started_at: new Date(Date.now() - 60_000).toISOString(),
					},
				],
				jobs: [{ id: 90, kind: "previews", subject: "part-1", attempt: 1 }],
				backlogs: [{ kind: "previews", left: 8_022, done: 585 }],
				scans: [
					{
						library_id: "l-films",
						phase: "reading",
						done: 3,
						known: 10,
						folder: "Heat (1995)",
					},
				],
				playbacks: [playing],
			} satisfies Schemas["Snapshot"]);
			let done = 3;
			let previews = 585;
			timer = setInterval(() => {
				done = Math.min(done + 5, 40);
				previews += 1;
				send("scan.progress", {
					kind: "scan.progress",
					at: new Date().toISOString(),
					library_id: "l-films",
					details: { phase: "reading", done, known: 40, folder: "Heat (1995)" },
				} satisfies Schemas["Event"]);
				send("jobs.progress", {
					kind: "jobs.progress",
					at: new Date().toISOString(),
					details: {
						job_kind: "previews",
						left: 8_607 - previews,
						done: previews,
					},
				} satisfies Schemas["Event"]);
			}, 400);
		},
		cancel() {
			clearInterval(timer);
		},
	});
	return new Response(body, {
		headers: { "content-type": "text/event-stream" },
	});
}

export async function admin(
	request: Request,
	url: URL,
	me: Schemas["Profile"],
): Promise<Response | undefined> {
	const route = `${request.method} ${url.pathname}`;
	if (!url.pathname.startsWith("/api/v1/admin/")) return undefined;
	if (me.role !== "admin") {
		return Response.json(
			{
				title: "Forbidden",
				status: 403,
				code: "forbidden",
				detail: "only an admin may",
			},
			{ status: 403, headers: { "content-type": "application/problem+json" } },
		);
	}
	switch (route) {
		case "GET /api/v1/admin/locales":
			return json({
				languages: ["de-DE", "en-GB", "en-US", "fr-FR"],
				countries: ["DE", "GB", "IN", "US"],
			} satisfies Schemas["Locales"]);
		case "GET /api/v1/admin/server":
			return json(server);
		case "GET /api/v1/admin/events":
			return events();
		case "GET /api/v1/admin/playbacks":
			return json({
				items: [playing],
				transcodes: { active: 1, conversions: 0, limit: 2 },
			} satisfies Schemas["NowPlayingList"]);
		case "DELETE /api/v1/admin/playbacks/pb-1":
			return done();
		case "GET /api/v1/admin/activity": {
			const kind = url.searchParams.get("kind");
			const items = activity.filter((e) => !kind || e.kind === kind);
			return json({
				items,
				offset: 0,
				total: items.length,
			} satisfies Schemas["EventPage"]);
		}
		case "GET /api/v1/admin/history":
			return json({
				items: [
					{
						id: "h-1",
						profile_id: "p-kids",
						title: {
							id: "t-quiet",
							kind: "movie",
							title: "Quiet Hours",
							added_at: film.added_at,
							duration_ms: 6_000_000,
						},
						method: "direct",
						started_at: "2026-10-05T19:00:00Z",
						stopped_at: "2026-10-05T20:40:00Z",
						position_ms: 6_000_000,
					},
				],
				offset: 0,
				total: 1,
			} satisfies Schemas["HistoryEntryPage"]);
		case "GET /api/v1/admin/libraries":
			return json({
				items: [
					{
						...films,
						counts: {
							movies: 250,
							shows: 0,
							seasons: 0,
							episodes: 0,
							collections: 1,
						},
					},
				],
			} satisfies Schemas["AdminLibraryListingList"]);
		case "POST /api/v1/admin/libraries": {
			const body = (await request.json()) as Schemas["AddLibrary"];
			return json(
				{ ...films, id: "l-new", name: body.name, root: body.root },
				201,
			);
		}
		case "PATCH /api/v1/admin/libraries/l-new":
		case "PATCH /api/v1/admin/libraries/l-films":
			return json(films);
		case "POST /api/v1/admin/libraries/l-films/scan":
			return done(202);
		case "POST /api/v1/admin/libraries/l-films/refresh":
			return done(202);
		case "GET /api/v1/admin/folders": {
			const path = url.searchParams.get("path");
			if (!path) return json({ items: [{ name: "media", path: "/media" }] });
			if (path === "/media") {
				return json({
					path,
					parent: "/",
					items: [
						{ name: "films", path: "/media/films" },
						{ name: "shows", path: "/media/shows" },
					],
				} satisfies Schemas["FolderList"]);
			}
			return json({
				path,
				parent: "/media",
				items: [],
			} satisfies Schemas["FolderList"]);
		}
		case "GET /api/v1/admin/providers":
			return json({ items: providers });
		case "PATCH /api/v1/admin/providers/mdblist":
			return json({ ...providers[2], ready: true });
		case "GET /api/v1/admin/plugins":
			return json({ items: [] });
		case "POST /api/v1/admin/profiles":
			return json({ id: "p-new", name: "Guest", role: "member" }, 201);
		case "GET /api/v1/admin/profiles/p-kids/access":
			return json({
				max_age: 12,
				unrated: "block",
				libraries: [],
			} satisfies Schemas["Access"]);
		case "PUT /api/v1/admin/profiles/p-kids/access":
			return done();
		case "GET /api/v1/admin/tasks":
			return json({
				items: [
					{
						key: "backup_database",
						running: false,
						started_at: "2026-10-06T19:00:00Z",
						finished_at: "2026-10-06T19:00:05Z",
						result: "failed",
						error: "disk full",
						next_at: "2026-10-09T19:00:00Z",
					},
				],
			} satisfies Schemas["TaskList"]);
		case "POST /api/v1/admin/tasks/backup_database/run":
			return done(202);
		case "GET /api/v1/admin/maintenance":
			return json(maintenance);
		case "PUT /api/v1/admin/maintenance":
			maintenance = (await request.json()) as Schemas["Maintenance"];
			return json(maintenance);
		case "GET /api/v1/admin/network":
			return json(network);
		case "PUT /api/v1/admin/network":
			network = (await request.json()) as Schemas["Network"];
			return json(network);
		case "GET /api/v1/admin/jobs":
			return json({
				counts: [
					{ kind: "identify", state: "queued", count: 12 },
					{ kind: "identify", state: "dead", count: deadJobs.length },
				],
				dead: deadJobs,
			} satisfies Schemas["JobQueue"]);
		case "POST /api/v1/admin/jobs/41/retry":
			deadJobs.length = 0;
			return done(202);
		case "GET /api/v1/admin/webhooks":
			return json({ items: webhooks });
		case "POST /api/v1/admin/webhooks": {
			const body = (await request.json()) as Schemas["AddWebhook"];
			const hook = { id: "w-1", created_at: "2026-10-06T20:00:00Z", ...body };
			webhooks = [hook];
			return json(
				{ ...hook, secret: "S3CRET-ONCE" } satisfies Schemas["AddedWebhook"],
				201,
			);
		}
		case "POST /api/v1/admin/webhooks/w-1/test":
			return done(202);
		case "DELETE /api/v1/admin/webhooks/w-1":
			webhooks = [];
			return done();
		case "GET /api/v1/admin/keys":
			return json({ items: keys });
		case "POST /api/v1/admin/keys": {
			const { name } = (await request.json()) as Schemas["NewKey"];
			const at = "2026-10-07T20:00:00Z";
			keys = [
				{
					id: "k-1",
					name,
					profile: "Oliver",
					created_at: at,
					last_seen_at: at,
				},
			];
			return json(
				{ id: "k-1", token: "pst_ONCE" } satisfies Schemas["CreatedKey"],
				201,
			);
		}
		case "DELETE /api/v1/admin/keys/k-1":
			keys = [];
			return done();
		// Quiet Hours is t-quiet on its admin page and t-film on the home rows.
		case "PATCH /api/v1/admin/titles/t-quiet":
		case "PATCH /api/v1/admin/titles/t-film":
		case "PUT /api/v1/admin/versions/v-quiet/markers":
		case "PUT /api/v1/admin/titles/t-quiet/artwork/poster":
			if (route.endsWith("poster")) {
				chosenPoster = ((await request.json()) as Schemas["ChooseArtwork"]).id;
			}
			return done();
		case "GET /api/v1/admin/titles/t-quiet/artwork/candidates":
			return json({
				items: ["a-1", "a-2"].map((id) => ({
					id,
					source: "tmdb",
					language: "en",
					width: 1000,
					height: 1500,
					chosen: id === chosenPoster,
				})),
			} satisfies Schemas["ArtworkCandidateList"]);
		case "GET /api/v1/admin/titles/t-quiet/candidates":
		case "GET /api/v1/admin/titles/t-film/candidates":
			return json({
				items: [
					{
						id: "101",
						title: "Quiet Hours",
						year: 2018,
						overview: "A night shift at a radio station.",
					},
				],
			} satisfies Schemas["CandidateList"]);
		case "PUT /api/v1/admin/titles/t-quiet/match":
		case "PUT /api/v1/admin/titles/t-film/match":
		case "POST /api/v1/admin/titles/t-film/analysis":
		case "POST /api/v1/admin/titles/t-quiet/refresh":
			return done(202);
		case "DELETE /api/v1/admin/titles/t-film/match":
		case "PUT /api/v1/admin/titles/t-quiet/locale":
			return done(202);
		case "POST /api/v1/admin/titles/t-film/split":
			return done();
		case "DELETE /api/v1/admin/titles/t-film":
			return Response.json(
				{
					title: "Conflict",
					status: 409,
					code: "conflict",
					detail:
						"its library does not allow its titles' files to be deleted: allow it in the library's settings",
				},
				{
					status: 409,
					headers: { "Content-Type": "application/problem+json" },
				},
			);
		case "PUT /api/v1/admin/collections/t-box/members":
			return done();
	}
	return undefined;
}

// The signed-in routes the dashboard reads beside the admin ones.
export function adminTitles(route: string): Response | undefined {
	switch (route) {
		case "GET /api/v1/titles/t-quiet":
			return json(film);
		case "GET /api/v1/titles/t-box":
			return json(collection);
		case "GET /api/v1/titles/t-box/members":
			return json({ items: [] } satisfies Schemas["CardList"]);
		case "GET /api/v1/libraries/l-films/collections":
			return json({
				items: [
					{
						id: "t-box",
						kind: "collection",
						title: "Lighthouse Films",
						added_at: film.added_at,
						origin: "user",
					},
				],
				offset: 0,
				total: 1,
			} satisfies Schemas["CardPage"]);
		case "GET /api/v1/search":
			return json({
				items: [
					{
						id: "t-quiet",
						kind: "movie",
						title: "Quiet Hours",
						year: 2018,
						added_at: film.added_at,
					},
				],
				offset: 0,
				total: 1,
				people: [],
				people_total: 0,
			} satisfies Schemas["Search"]);
	}
	return undefined;
}
