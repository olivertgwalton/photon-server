// The Go server as the web sees it, for the e2e suite: the built app's files
// and the API on one origin, the answers a real server gave, typed by the same
// schema the app is, so a change to the API that the app would feel fails
// `bun run check` here too.
import type { components } from "../src/lib/api/schema.d.ts";
import { admin, adminTitles } from "./mock-admin.ts";

type Schemas = components["schemas"];

const ada: Schemas["Profile"] = { id: "p-ada", name: "Ada", role: "admin" };
const kids: Schemas["Profile"] = { id: "p-kids", name: "Kids", role: "user" };

const server: Schemas["Info"] = { id: "s-1", name: "Den", version: "v1.0.0" };

const libraries: Schemas["LibraryList"] = {
	items: [
		{ id: "l-films", name: "Films", kind: "movies" },
		{ id: "l-shows", name: "Shows", kind: "shows" },
	],
};

// The catalogue: a film with everything a title page draws, a show of two
// episodes, a box set, and enough plain films to page a wall.
const person = "5f0c1d8e-2b1a-4c3d-9e8f-0a1b2c3d4e5f";
const guest = "6a1d2e9f-3c2b-4d4e-8f9a-1b2c3d4e5f6a";
const art = "0b4e2f1a-9c8d-4e7f-a6b5-c4d3e2f1a0b9";

type State = Schemas["TitleState"];
const states = new Map<string, State>([
	["t-ep", { position_ms: 600_000 }],
	["t-film", { position_ms: 1_200_000 }],
]);

const base = (
	id: string,
	kind: Schemas["ItemKind"],
	title: string,
	extra: Partial<Schemas["Card"]> = {},
): Schemas["Card"] => ({
	id,
	kind,
	title,
	added_at: "2026-10-01T20:00:00Z",
	...extra,
});

const films: Schemas["Card"][] = [
	base("t-film", "movie", "Quiet Hours", {
		year: 2018,
		duration_ms: 6_720_000,
		version_count: 2,
		poster: art,
		backdrop: art,
		blurhashes: { [art]: "LEHV6nWB2yk8pyo0adR*.7kCMdnj" },
	}),
	...Array.from({ length: 249 }, (_, i) =>
		base(
			`t-f${i}`,
			"movie",
			`${"BCDEFGH"[i % 7]}ilm ${String(i).padStart(3, "0")}`,
			{
				year: 1990 + (i % 30),
			},
		),
	),
];
// The wall's order, where `films` is the order they were added.
const byTitle = (cards: Schemas["Card"][]) =>
	[...cards].sort((a, b) => a.title.localeCompare(b.title));

const shows = [base("t-show", "show", "Small Show", { year: 2020 })];
const episode = (id: string, n: number, title: string) =>
	base(id, "episode", title, {
		duration_ms: 1_800_000,
		show: { id: "t-show", title: "Small Show" },
		season: { id: "t-s1", title: "Season 1" },
		season_number: 1,
		episode_number: n,
	});
const episodes = [episode("t-ep", 1, "Pilot"), episode("t-ep2", 2, "Second")];
const collection = base("c-set", "collection", "Quiet Collection", {
	origin: "tmdb",
});

const card = (c: Schemas["Card"]): Schemas["Card"] => {
	const state = states.get(c.id);
	return state ? { ...c, state } : c;
};
const season = base("t-s1", "season", "Season 1", {
	show: { id: "t-show", title: "Small Show" },
});
const everything = () => [...films, ...shows, season, ...episodes, collection];
const byID = (id: string) => everything().find((c) => c.id === id);

const homeRows = (): Schemas["HomeRow"][] => [
	{
		kind: "continue_watching",
		title: "Continue Watching",
		items: everything().filter((c) => states.get(c.id)?.position_ms),
	},
	{
		kind: "watchlist",
		title: "Watchlist",
		items: everything().filter((c) => states.get(c.id)?.watchlisted_at),
	},
	{
		kind: "favourites",
		title: "Favourites",
		items: everything().filter((c) => states.get(c.id)?.favourite_at),
	},
	{
		kind: "recently_added_films",
		title: "Recently Added in Films",
		library: { id: "l-films", name: "Films" },
		items: films,
	},
];

// The server's rows, in the reader's order and leaving out those hidden.
function home(arranged: Schemas["HomeSection"][], limit = 20): Schemas["Home"] {
	const rows = homeRows();
	return {
		rows: arranged
			.filter((s) => s.visibility === "shown")
			.flatMap((s) => rows.filter((r) => r.kind === s.row))
			.map((r) => ({ ...r, items: r.items.slice(0, limit).map(card) }))
			.filter((r) => r.items.length),
	};
}

const facets: Schemas["Facets"] = {
	genres: ["Comedy", "Drama"],
	years: [2018, 2019],
	certificates: [
		{ name: "12A", value: "12A" },
		{ name: "15", value: "15" },
	],
	studios: ["Ealing"],
	resolutions: ["1080p", "4k"],
	ranges: ["sdr", "dv"],
	rating_sites: ["imdb", "tmdb"],
	marks: ["watched", "unwatched", "in_progress", "favourite", "watchlist"],
};

const stream = (
	index: number,
	kind: Schemas["StreamKind"],
	extra: Partial<Schemas["StreamPage"]>,
): Schemas["StreamPage"] => ({
	index,
	kind,
	codec: "h264",
	display_title: `Track ${index}`,
	...extra,
});

function page(id: string): Schemas["TitlePage"] | undefined {
	const c = byID(id);
	if (!c) return;
	const out: Schemas["TitlePage"] = {
		...card(c),
		library_id: ["show", "season", "episode"].includes(c.kind)
			? "l-shows"
			: "l-films",
		artwork: c.poster ? { poster: [art], backdrop: [art] } : {},
	};
	if (id === "t-film") {
		Object.assign(out, {
			overview: "A night shift at a radio station.",
			tagline: "Nobody is listening.",
			certificate: "15",
			qualified_certificate: "15",
			genres: ["Drama"],
			studios: ["Ealing"],
			ids: { imdb: "tt0000001", tmdb: "1" },
			ratings: [
				{ site: "imdb", score: 78, votes: 1200 },
				{ site: "rotten_tomatoes", score: 93 },
			],
			collections: [{ id: "c-set", title: "Quiet Collection", poster: art }],
			// As a provider credits one person for several jobs.
			credits: [
				{ person_id: person, name: "Ada Lane", kind: "creator" },
				{ person_id: person, name: "Ada Lane", kind: "actor", role: "Host" },
				{ person_id: person, name: "Ada Lane", kind: "director" },
				{ person_id: person, name: "Ada Lane", kind: "writer" },
				{ person_id: person, name: "Ada Lane", kind: "writer" },
				{
					person_id: guest,
					name: "Ben Hale",
					kind: "guest_star",
					role: "Caller",
				},
			],
			extras: [
				{
					id: "t-trailer",
					extra_kind: "trailer",
					title: "Trailer",
					duration_ms: 150_000,
					image: "/api/v1/parts/p-trailer/chapters/0/image",
				},
				{ id: "t-scene", extra_kind: "deleted_scene", title: "The Lost Call" },
				{ id: "t-blooper", extra_kind: "blooper", title: "Outtakes" },
			],
			videos: [
				{
					extra_kind: "featurette",
					site: "YouTube",
					key: "quiet",
					name: "Making Quiet Hours",
					thumb: art,
				},
			],
			versions: [
				{
					id: "v-4k",
					display_title: "4K (HEVC Dolby Vision)",
					label: "4K",
					container: "mkv",
					duration_ms: 6_720_000,
					size_bytes: 40_000_000_000,
					bitrate_kbps: 48_000,
					parts: 1,
					files: [
						{
							id: "p-4k",
							index: 0,
							size_bytes: 40_000_000_000,
							duration_ms: 6_720_000,
							offset_ms: 0,
						},
					],
					streams: [
						stream(0, "video", {
							display_title: "4K (HEVC Dolby Vision)",
							codec: "hevc",
							width: 3840,
							height: 2160,
							range: "dv",
							dv_profile: 8,
						}),
						stream(1, "audio", {
							display_title: "English (Dolby TrueHD 7.1)",
							codec: "truehd",
							language: "eng",
							channel_layout: "7.1",
							default: true,
						}),
						stream(2, "audio", {
							display_title: "English Commentary (Dolby Digital Stereo)",
							codec: "ac3",
							language: "eng",
							channels: 2,
							commentary: true,
						}),
						stream(3, "subtitle", {
							display_title: "French (SRT)",
							codec: "subrip",
							language: "fra",
						}),
					],
					chapters: [
						{ start_ms: 0, end_ms: 600_000, title: "Sign on" },
						{ start_ms: 600_000, end_ms: 6_720_000 },
					],
				},
				{
					id: "v-hd",
					display_title: "1080p (H.264)",
					label: "1080p",
					container: "mp4",
					duration_ms: 6_720_000,
					size_bytes: 8_000_000_000,
					// In two files, as a film on two discs is.
					parts: 2,
					files: [
						{
							id: "p-hd",
							index: 0,
							size_bytes: 4_000_000_000,
							duration_ms: 3_360_000,
							offset_ms: 0,
						},
						{
							id: "p-hd2",
							index: 1,
							size_bytes: 4_000_000_000,
							duration_ms: 3_360_000,
							offset_ms: 3_360_000,
						},
					],
					streams: [
						stream(0, "video", {
							display_title: "1080p (H.264)",
							width: 1920,
							height: 1080,
						}),
						stream(1, "audio", {
							display_title: "English (AAC Stereo)",
							codec: "aac",
							language: "eng",
							channels: 2,
						}),
					],
				},
			],
		});
	}
	if (id === "t-show") {
		out.themes = ["th-small-show"];
		out.seasons = [
			{
				id: "t-s1",
				number: 1,
				title: "Season 1",
				episodes: 2,
				state: { unwatched: 2 },
			},
		];
	}
	if (id === "t-s1") {
		out.show = { id: "t-show", title: "Small Show" };
		out.episodes = episodes.map((e) => ({
			id: e.id,
			title: e.title,
			episode_number: e.episode_number,
			duration_ms: e.duration_ms,
			state: states.get(e.id),
		}));
	}
	if (c.kind === "episode") {
		out.season = { id: "t-s1", title: "Season 1" };
		out.versions = [
			{
				id: `v-${id}`,
				display_title: "H.264",
				container: "mkv",
				duration_ms: 1_800_000,
				size_bytes: 1_000_000_000,
				parts: 1,
				files: [
					{
						id: `p-${id}`,
						index: 0,
						size_bytes: 1_000_000_000,
						duration_ms: 1_800_000,
						offset_ms: 0,
					},
				],
				streams: [stream(0, "video", {}), stream(1, "audio", { codec: "aac" })],
			},
		];
	}
	return out;
}

let playlists: (Schemas["Playlist"] & { items: string[] })[] = [];
const downloads: Schemas["Download"][] = [];

// The change feed's open streams, and a way for a test to speak on them.
const feeds = new Set<ReadableStreamDefaultController<string>>();

// The words the server names its values by, as it answers in English.
const vocabulary: Schemas["Vocabulary"] = {
	tasks: {
		backfill_previews: {
			name: "Make previews",
			description:
				"Makes the chapter images and seek previews libraries ask for, and clears unused ones.",
		},
		backup_database: {
			name: "Back up the database",
			description: "Dumps the database for pg_restore, keeping the newest few.",
		},
		detect_markers: {
			name: "Detect intros and credits",
			description:
				"Compares the sound of each season's episodes to find what they share, and finds where each film's picture goes dark for its credits.",
		},
		fetch_subtitles: {
			name: "Download missing subtitles",
			description:
				"Fetches subtitles in the languages libraries name for copies with none in them.",
		},
		prune_activity: {
			name: "Prune the activity log",
			description: "Forgets activity older than 30 days.",
		},
		refresh_collections: {
			name: "Refresh smart collections",
			description:
				"Finds what each smart collection's filters hold again, catching what was matched or edited since.",
		},
		refresh_metadata: {
			name: "Refresh metadata",
			description:
				"Asks the providers again about titles whose libraries say it is time.",
		},
		scan_libraries: {
			name: "Scan libraries",
			description:
				"Reads every library's folders for what is new, changed or gone.",
		},
		sweep_artwork: {
			name: "Clear old artwork",
			description:
				"Removes replaced pictures from the cache, takes the blur drawn while each loads where it has none, and fetches the pictures titles show that the cache lacks.",
		},
		sweep_downloads: {
			name: "Clear old downloads",
			description:
				"Forgets downloads kept past their time, and conversions nothing needs.",
		},
		sweep_jobs: {
			name: "Requeue stalled jobs",
			description: "Puts back the jobs a node stopped working on.",
		},
		sync_lists: {
			name: "Sync list collections",
			description:
				"Reads each list collection's TMDB or MDBList list again and keeps the titles of it the library has.",
		},
	},
	jobs: {
		convert: "Convert for download",
		deliver_webhook: "Send a webhook",
		identify: "Identify",
		import_history: "Import watch history",
		keyframe_walk: "Walk files for keyframes",
		keyframes: "Read keyframes",
		markers: "Find intros and credits",
		previews: "Make previews",
		probe: "Read media info",
		scan_library: "Scan a library",
		theme: "Fetch a theme tune",
	},
	rows: {
		collection: "Collections",
		continue_watching: "Continue Watching",
		favourites: "Favourites",
		next_up: "Next Up",
		recently_added_films: "Recently Added Films",
		recently_added_shows: "Recently Added Shows",
		recently_released: "Recently Released",
		top_rated_unwatched: "Top Rated",
		watchlist: "Watchlist",
	},
	roles: {
		admin: "Admin",
		manager: "Manager",
		user: "User",
	},
	markers: {
		credits: "Credits",
		intro: "Intro",
		preview: "Preview",
		recap: "Recap",
	},
	extras: {
		behind_the_scenes: "Behind the scenes",
		blooper: "Blooper",
		clip: "Clip",
		deleted_scene: "Deleted scene",
		featurette: "Featurette",
		interview: "Interview",
		other: "Extra",
		scene: "Scene",
		short: "Short",
		teaser: "Teaser",
		theme_video: "Theme video",
		trailer: "Trailer",
	},
	reasons: {
		audio_channels_not_supported: {
			name: "Audio channels",
			description: "The audio has more channels than the player plays.",
		},
		audio_codec_not_supported: {
			name: "Audio codec",
			description: "The player doesn't play the audio's codec.",
		},
		bitrate_exceeds_limit: {
			name: "Bitrate",
			description: "The file is above the quality chosen.",
		},
		container_not_supported: {
			name: "Container",
			description: "The player doesn't open the file's container.",
		},
		parts_not_supported: {
			name: "Several files",
			description: "The title is in several files, played as one.",
		},
		subtitle_codec_not_supported: {
			name: "Subtitles",
			description: "The subtitles are pictures, drawn into the video.",
		},
		video_bit_depth_not_supported: {
			name: "Bit depth",
			description: "The video's bit depth is more than the player plays.",
		},
		video_codec_not_supported: {
			name: "Video codec",
			description: "The player doesn't play the video's codec.",
		},
		video_level_not_supported: {
			name: "Video level",
			description: "The video's level is higher than the player plays.",
		},
		video_profile_not_supported: {
			name: "Video profile",
			description: "The player doesn't play the video's profile.",
		},
		video_range_not_supported: {
			name: "HDR",
			description: "The screen doesn't show the video's HDR.",
		},
		video_resolution_not_supported: {
			name: "Resolution",
			description: "The picture is larger than the player plays.",
		},
	},
	accelerations: {
		nvenc: "NVENC",
		qsv: "Quick Sync",
		software: "Software",
		vaapi: "VA-API",
		videotoolbox: "VideoToolbox",
	},
	node_roles: {
		all: {
			name: "Serves and transcodes",
			description: "As every server on its own does.",
		},
		serve: {
			name: "Serves only",
			description:
				"Transcodes nothing, for playback or downloads, leaving that to the others.",
		},
		transcode: {
			name: "Transcodes first",
			description:
				"Asked to transcode before any other, while it has room: the server with the GPU. It serves people too.",
		},
	},
	import_sources: {
		emby: "Emby",
		jellyfin: "Jellyfin",
		plex: "Plex",
	},
	import_misses: {
		no_ids: "No TMDB, TheTVDB or IMDb id",
		not_found: "Not in a library here",
		undated: "No date watched",
	},
	play_methods: {
		direct: "Direct play",
		remux: "Direct stream",
		transcode: "Transcode",
	},
	rating_sites: {
		imdb: "IMDb",
		rotten_tomatoes: "Rotten Tomatoes",
		rotten_tomatoes_audience: "RT Audience",
		tmdb: "TMDB",
	},
	ranges: {
		dv: "Dolby Vision",
		hdr10: "HDR10",
		hdr10plus: "HDR10+",
		hlg: "HLG",
		sdr: "SDR",
	},
	resolutions: {
		"1080p": "1080p",
		"4k": "4K",
		"720p": "720p",
		sd: "SD",
	},
	kinds: {
		collection: "Collections",
		episode: "Episodes",
		extra: "Extras",
		movie: "Films",
		season: "Seasons",
		show: "Shows",
	},
	library_kinds: {
		movies: "Films",
		shows: "Shows",
	},
	stream_kinds: {
		audio: "Audio",
		subtitle: "Subtitle",
		video: "Video",
	},
	marks: {
		favourite: "Favourites",
		in_progress: "In progress",
		unwatched: "Unwatched",
		watched: "Watched",
		watchlist: "Watchlist",
	},
	milestones: {
		season_finale: "Finale",
		season_premiere: "Season premiere",
		series_premiere: "Series premiere",
	},
	calendar_filters: {
		all: "Everything",
		favourites: "Favourites",
		mine: "My titles",
		watchlist: "Watchlist",
	},
	sorts: {
		added: {
			name: "Date added",
			ascending: "Oldest first",
			descending: "Newest first",
		},
		played: {
			name: "Last played",
			ascending: "Longest ago",
			descending: "Most recent",
		},
		rating: {
			name: "Rating",
			ascending: "Lowest first",
			descending: "Highest first",
		},
		released: {
			name: "Release date",
			ascending: "Oldest first",
			descending: "Newest first",
		},
		runtime: {
			name: "Runtime",
			ascending: "Shortest first",
			descending: "Longest first",
		},
		title: {
			name: "Title",
			ascending: "A to Z",
			descending: "Z to A",
		},
	},
	job_states: {
		dead: "Gave up",
		queued: "Queued",
		rerun: "To run again",
		running: "Running",
	},
	download_states: {
		converting: "Converting",
		failed: "Failed",
		queued: "Waiting",
		ready: "Ready",
	},
	logged: {
		"auth.sign_in_refused": "Refused sign-ins",
		"auth.signed_in": "Sign-ins",
		"backup.made": "Backups",
		"job.dead": "Dead jobs",
		"library.added": "Libraries added",
		"library.removed": "Libraries removed",
		"library.scanned": "Scans",
		"library.titles_added": "Titles added",
		"playback.started": "Plays started",
		"playback.stopped": "Plays stopped",
		"profile.added": "Profiles added",
		"profile.removed": "Profiles removed",
		"task.failed": "Failed tasks",
	},
	hookable: {
		"auth.sign_in_refused": "A sign-in is refused",
		"auth.signed_in": "Someone signs in",
		"backup.made": "A backup is made",
		"library.added": "A library is added",
		"library.removed": "A library is removed",
		"library.scanned": "A library is scanned",
		"library.titles_added": "Titles are added",
		"playback.paused": "A play is paused",
		"playback.resumed": "A play is resumed",
		"playback.started": "A play starts",
		"playback.stopped": "A play stops",
		"profile.added": "A profile is added",
		"profile.removed": "A profile is removed",
		"task.failed": "A task fails",
	},
};

const json = (request: Request) => request.json() as Promise<never>;
const none = () => new Response(null, { status: 204 });

// A film and two episodes to play, each six seconds of e2e/fixtures/film.mp4.
const version = (id: string): Schemas["VersionPage"] => ({
	id,
	display_title: "SD (H.264)",
	container: "mov,mp4,m4a,3gp,3g2,mj2",
	duration_ms: 6_000,
	size_bytes: 152_341,
	bitrate_kbps: 200,
	parts: 1,
	files: [
		{
			id: `p-${id}`,
			index: 0,
			size_bytes: 152_341,
			duration_ms: 6_000,
			offset_ms: 0,
		},
	],
	streams: [
		{
			index: 0,
			kind: "video",
			codec: "h264",
			profile: "High",
			width: 320,
			height: 180,
			resolution: "sd",
			display_title: "SD (H.264)",
		},
		{
			index: 1,
			kind: "audio",
			codec: "aac",
			language: "en",
			channels: 2,
			display_title: "English (AAC Stereo)",
		},
		{
			index: 2,
			kind: "audio",
			codec: "aac",
			language: "fr",
			channels: 2,
			display_title: "French (AAC Stereo)",
		},
	],
	subtitles: [
		{
			id: "0199b3c0-0000-7000-8000-0000000000d1",
			codec: "subrip",
			kind: "text",
			language: "en",
			display_title: "English (SRT External)",
		},
	],
	chapters: [
		{ start_ms: 0, end_ms: 3_000, title: "Opening" },
		{ start_ms: 3_000, end_ms: 6_000, title: "The Rest" },
	],
	markers: [
		{ kind: "intro", start_ms: 500, end_ms: 3_000, source: "chapter" },
		{ kind: "credits", start_ms: 4_000, end_ms: 6_000, source: "chapter" },
	],
	trickplay: [
		{
			part_id: "part-1",
			offset_ms: 0,
			width: 160,
			height: 90,
			interval_ms: 1_000,
			columns: 3,
			rows: 2,
			thumbnails: 6,
			sheets: 1,
		},
	],
});

const titles: Record<string, Schemas["TitlePage"]> = {
	"p-film": {
		id: "p-film",
		library_id: "l-films",
		kind: "movie",
		title: "Quiet Hours",
		added_at: "2026-10-01T20:00:00Z",
		versions: [version("v-film")],
		state: { position_ms: 1_000 },
	},
	"p-ep": {
		id: "p-ep",
		library_id: "l-shows",
		kind: "episode",
		title: "Pilot",
		added_at: "2026-10-01T20:00:00Z",
		show: { id: "t-show", title: "Small Show" },
		season_number: 1,
		episode_number: 1,
		versions: [version("v-ep")],
	},
	"p-ep2": {
		id: "p-ep2",
		library_id: "l-shows",
		kind: "episode",
		title: "Second",
		added_at: "2026-10-01T20:00:00Z",
		show: { id: "t-show", title: "Small Show" },
		season_number: 1,
		episode_number: 2,
		versions: [version("v-ep2")],
	},
	// Signs in ASS inside the file, and a font of its own.
	"p-anime": {
		id: "p-anime",
		library_id: "l-films",
		kind: "movie",
		title: "Bakery Street",
		added_at: "2026-10-01T20:00:00Z",
		versions: [
			{
				...version("v-anime"),
				streams: [
					...version("v-anime").streams,
					{
						index: 3,
						kind: "subtitle",
						codec: "ass",
						subtitle_kind: "styled",
						title: "Signs",
						display_title: "Signs (ASS)",
					},
				],
				subtitles: [],
			},
		],
	},
	"t-busy": {
		id: "t-busy",
		library_id: "l-films",
		kind: "movie",
		title: "Busy Night",
		added_at: "2026-10-01T20:00:00Z",
		versions: [version("v-busy")],
	},
	"t-odd": {
		id: "t-odd",
		library_id: "l-films",
		kind: "movie",
		title: "Odd Format",
		added_at: "2026-10-01T20:00:00Z",
		versions: [version("v-odd")],
	},
};

// Where each playback is of which title, and where the reader stopped.
const playing = new Map<string, string>();
const stopped = new Map<string, number>();

// The server's answer to a play: the file as it is, or, under a quality
// limit, HLS of it.
function play(id: string, body: Schemas["Play"]): Response {
	if (id === "t-busy") {
		return Response.json(
			{
				title: "Service Unavailable",
				status: 503,
				code: "transcode_limit",
				detail:
					"the server is already transcoding as many videos at once as it may: 1",
			} satisfies Schemas["Problem"],
			{ status: 503, headers: { "content-type": "application/problem+json" } },
		);
	}
	if (id === "t-odd") {
		return Response.json(
			{
				title: "Unprocessable Entity",
				status: 422,
				code: "no_compatible_stream",
				reasons: ["video_codec_not_supported", "audio_codec_not_supported"],
			} satisfies Schemas["Refusal"],
			{ status: 422, headers: { "content-type": "application/problem+json" } },
		);
	}
	const limited = (body.profile?.max_bitrate_kbps ?? 0) > 0;
	const playback: Schemas["Playback"] = {
		playback_id: crypto.randomUUID(),
		method: limited ? "transcode" : "direct",
		version_id: titles[id]?.versions?.[0]?.id ?? "",
		video: limited
			? {
					stream: 0,
					decision: "transcode",
					codec: "h264",
					width: 320,
					height: 180,
					bitrate_kbps: 300,
				}
			: { stream: 0, decision: "copy" },
		audio: { stream: body.audio_stream ?? 1, decision: "copy" },
		reasons: limited ? ["bitrate_exceeds_limit"] : undefined,
		expires_at: "2026-10-07T00:00:00Z",
	};
	playing.set(playback.playback_id, id);
	if (limited) playback.playlist = "/api/v1/hls/pb/1/sig/main.m3u8";
	else {
		playback.parts = [
			{
				id: "part-1",
				url: "/api/v1/parts/part-1/stream?exp=1&sig=s",
				offset_ms: 0,
				duration_ms: 6_000,
			},
		];
		playback.subtitles =
			id === "p-anime"
				? [
						{
							stream: 3,
							codec: "ass",
							kind: "styled",
							title: "Signs",
							url: "/api/v1/parts/part-1/subtitles/3?exp=1&sig=s",
							fonts: "/api/v1/parts/part-1/fonts?exp=1&sig=s",
						},
					]
				: [
						{
							id: "0199b3c0-0000-7000-8000-0000000000d1",
							codec: "subrip",
							kind: "text",
							language: "en",
							url: "/api/v1/subtitles/sub-1/file?exp=1&sig=s",
						},
					];
	}
	return Response.json(playback);
}

const fixture = (name: string) =>
	new Response(Bun.file(new URL(`fixtures/${name}`, import.meta.url)));

const problem = (status: number, code: Schemas["ProblemCode"], title: string) =>
	Response.json({ title, status, code } satisfies Schemas["Problem"], {
		status,
		headers: { "content-type": "application/problem+json" },
	});

// A 1×1 PNG, for every picture.
const pixel = Uint8Array.from(
	atob(
		"iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAYAAAAfFcSJAAAADUlEQVR42mNkqGcAAAIEAQDeEycgAAAAAElFTkSuQmCC",
	),
	(c) => c.charCodeAt(0),
);

// A picture is kept where it is a PNG, as the server keeps only what its
// bytes say is a picture; each gets a new id, as the server's do.
async function avatar(profile: Schemas["Profile"], request: Request) {
	const bytes = new Uint8Array(await request.arrayBuffer());
	if (bytes[1] !== 0x50 || bytes[2] !== 0x4e || bytes[3] !== 0x47) {
		return problem(
			400,
			"invalid_body",
			"not a picture: a JPEG, PNG, GIF or WebP is kept",
		);
	}
	profile.avatar = crypto.randomUUID();
	return Response.json(profile);
}

// token → who it is watching as, and whether the profile has a PIN.
const sessions = new Map<string, Schemas["Profile"]>();
let kidsPIN = "";

// token → how its reader plays: each sign-in starts from the defaults, so
// one test's choices are not the next's.
const defaults: Schemas["Preferences"] = {
	audio_language: "",
	audio_track: "default",
	subtitle_language: "",
	subtitle_mode: "default",
	remember_audio: "remember",
	remember_subtitles: "remember",
	max_bitrate_kbps: 0,
	next_episode: "play",
	intro_action: "ask",
	credits_action: "ask",
	theme_music: "off",
	home: [
		"continue_watching",
		"next_up",
		"watchlist",
		"favourites",
		"recently_added_films",
		"recently_added_shows",
		"recently_released",
		"top_rated_unwatched",
	].map((row) => ({
		row: row as Schemas["HomeRowKind"],
		visibility: "shown" as const,
	})),
};
const preferences = new Map<string, Schemas["Preferences"]>();
// Each profile's library order, as the server keeps it.
const libraryOrders = new Map<string, string[]>();

const cookie = "photon_session";

// The app's page for every path that is not a file, as the server answers it.
async function app(url: URL) {
	const file = Bun.file(`build${url.pathname}`);
	if (url.pathname !== "/" && (await file.exists())) return new Response(file);
	return new Response(Bun.file("build/index.html"));
}

const server_ = Bun.serve({
	port: Number(process.env.PORT ?? 4173),
	async fetch(request) {
		const url = new URL(request.url);
		if (!/^\/(api|mock)\//.test(url.pathname)) return app(url);
		const route = `${request.method} ${url.pathname}`;
		const token =
			request.headers.get("authorization")?.replace("Bearer ", "") ??
			new Bun.CookieMap(request.headers.get("cookie") ?? "").get(cookie) ??
			undefined;
		const me = token ? sessions.get(token) : undefined;

		if (route === "GET /api/v1/server") return Response.json(server);
		// Not the API: how a test knows a page is listening, and makes the server
		// announce a change.
		if (route === "GET /mock/listening") return Response.json(feeds.size);
		if (route === "POST /mock/emit") {
			const { event, data, add } = (await request.json()) as {
				event: string;
				data: object;
				add?: string;
			};
			if (add) films.unshift(base(`t-new${films.length}`, "movie", add));
			for (const feed of feeds) {
				feed.enqueue(`event: ${event}\ndata: ${JSON.stringify(data)}\n\n`);
			}
			return none();
		}
		if (
			request.method === "GET" &&
			(url.pathname.startsWith("/api/v1/artwork/") ||
				url.pathname.endsWith("/image"))
		) {
			return new Response(pixel, { headers: { "content-type": "image/png" } });
		}
		if (route === "POST /api/v1/auth/login") {
			const body = (await request.json()) as Schemas["LoginRequest"];
			if (
				body.method !== "password" ||
				body.name !== "Ada" ||
				body.password !== "correct horse"
			) {
				return problem(401, "sign_in_refused", "Unauthorized");
			}
			const issued = crypto.randomUUID();
			sessions.set(issued, ada);
			if (body.keep !== "cookie") {
				return Response.json({
					token: issued,
					profile: ada,
				} satisfies Schemas["LoginResponse"]);
			}
			return Response.json(
				{ profile: ada } satisfies Schemas["LoginResponse"],
				{
					headers: {
						"set-cookie": `${cookie}=${issued}; Path=/; HttpOnly; SameSite=Lax`,
					},
				},
			);
		}
		if (!me) return problem(401, "unauthenticated", "Unauthorized");

		const title = url.pathname.match(
			/^\/api\/v1\/titles\/([^/]+)(\/play|\/next)?$/,
		);
		if (title?.[1] && titles[title[1]]) {
			const [, id, action] = title;
			if (!action && request.method === "GET") {
				const at = stopped.get(id);
				// The server chooses the sound in the reader's language, as it does
				// where they ask for it over the file's default.
				const prefs = preferences.get(token as string) ?? defaults;
				const page = {
					...titles[id],
					versions: titles[id].versions?.map((v) => ({
						...v,
						default_audio_stream:
							prefs.audio_track === "language"
								? v.streams.find(
										(s) =>
											s.kind === "audio" && s.language === prefs.audio_language,
									)?.index
								: undefined,
					})),
				};
				return Response.json(
					at === undefined ? page : { ...page, state: { position_ms: at } },
				);
			}
			if (action === "/play" && request.method === "POST") {
				return play(id, (await request.json()) as Schemas["Play"]);
			}
			if (action === "/next" && id === "p-ep") {
				const { versions: _, ...card } = titles["p-ep2"];
				return Response.json(card satisfies Schemas["Card"]);
			}
			if (action === "/next") return problem(404, "not_found", "Not Found");
		}
		const report = route.match(
			/^POST \/api\/v1\/playbacks\/([^/]+)\/(progress|stop)$/,
		);
		if (report?.[1]) {
			const { position_ms } = (await request.json()) as Schemas["Position"];
			const id = playing.get(report[1]);
			if (id && report[2] === "stop") stopped.set(id, position_ms);
			return Response.json({ reach: "resumable" } satisfies Schemas["Reached"]);
		}
		const hls = url.pathname.match(/^\/api\/v1\/hls\/pb\/1\/sig\/([\w.]+)$/);
		if (hls) return fixture(`hls/${hls[1]}`);
		switch (route) {
			case "GET /api/v1/titles/t-film/subtitles/search":
				return Response.json({
					version_id: "v-4k",
					items: [
						{
							source: "opensubtitles",
							id: "9",
							language: url.searchParams.get("language") ?? "",
							release: "Quiet.Hours.2160p",
							for_release: true,
							downloads: 1200,
						},
					],
				} satisfies Schemas["FoundSubtitles"]);
			case "POST /api/v1/titles/t-film/subtitles":
				return Response.json({ id: "sub-9" } satisfies Schemas["Created"], {
					status: 201,
				});
			case "GET /api/v1/parts/part-1/stream":
				return fixture("film.mp4");
			case "GET /api/v1/subtitles/sub-1/file":
				return fixture("film.srt");
			case "GET /api/v1/parts/part-1/subtitles/3":
				return fixture("film.ass");
			case "GET /api/v1/parts/part-1/fonts":
				return Response.json({
					fonts: [
						{
							name: "2.woff2",
							url: "/api/v1/parts/part-1/fonts/2.woff2?exp=1&sig=s",
						},
					],
				} satisfies Schemas["Fonts"]);
			// JASSUB's own Liberation Sans stands in for a font the file carries.
			case "GET /api/v1/parts/part-1/fonts/2.woff2":
				return new Response(Bun.file("node_modules/jassub/dist/default.woff2"));
			case "GET /api/v1/parts/part-1/trickplay/0":
				return fixture("sheet.jpg");
			case "GET /api/v1/profile":
				return Response.json(me);
			case "POST /api/v1/profile/avatar":
				return avatar(me, request);
			case "DELETE /api/v1/profile/avatar":
				delete me.avatar;
				return new Response(null, { status: 204 });
			case "PATCH /api/v1/profile": {
				const { name } = (await request.json()) as Schemas["Name"];
				if ([ada, kids].some((p) => p !== me && p.name === name.trim())) {
					return problem(
						409,
						"conflict",
						"a profile with that name already exists",
					);
				}
				me.name = name.trim();
				return Response.json(me);
			}
			case "POST /api/v1/auth/logout":
				sessions.delete(token as string);
				return new Response(null, {
					status: 204,
					headers: { "set-cookie": `${cookie}=; Path=/; Max-Age=0` },
				});
			case "GET /api/v1/profiles":
				return Response.json({
					items: [
						{ ...ada, lock: "password" },
						{ ...kids, lock: kidsPIN ? "pin" : "password" },
					],
				} satisfies Schemas["ProfileListingList"]);
			case `POST /api/v1/profiles/${ada.id}/switch`:
			case `POST /api/v1/profiles/${kids.id}/switch`: {
				const sent = await request.text();
				const body = (sent ? JSON.parse(sent) : {}) as Schemas["Switch"];
				const target = ada.id === url.pathname.split("/")[4] ? ada : kids;
				const locked =
					target === kids ? kidsPIN || "crayon box" : "correct horse";
				if (target !== me && locked && body.secret !== locked) {
					return problem(403, "wrong_secret", "Forbidden");
				}
				sessions.set(token as string, target);
				return Response.json(target);
			}
			case "PUT /api/v1/profile/pin": {
				const body = (await request.json()) as Schemas["Pin"];
				if (!/^\d{4,6}$/.test(body.pin)) {
					return Response.json(
						{
							title: "Bad Request",
							status: 400,
							code: "invalid_body",
							detail: "a PIN is 4 to 6 digits",
						} satisfies Schemas["Problem"],
						{
							status: 400,
							headers: { "content-type": "application/problem+json" },
						},
					);
				}
				kidsPIN = body.pin;
				return new Response(null, { status: 204 });
			}
			case "PUT /api/v1/profile/password": {
				const body = (await request.json()) as Schemas["PasswordChange"];
				return body.current === "correct horse"
					? new Response(null, { status: 204 })
					: problem(403, "wrong_secret", "That isn't the current password.");
			}
			case "DELETE /api/v1/profile/pin":
				kidsPIN = "";
				return new Response(null, { status: 204 });
			case "GET /api/v1/profile/preferences":
				return Response.json(preferences.get(token as string) ?? defaults);
			case "PATCH /api/v1/profile/preferences": {
				const change = (await request.json()) as Schemas["PreferencesChange"];
				const kept: Schemas["Preferences"] = {
					...(preferences.get(token as string) ?? defaults),
					...Object.fromEntries(
						Object.entries(change).filter(([, v]) => v !== undefined),
					),
					saved_at: new Date().toISOString(),
				};
				preferences.set(token as string, kept);
				return Response.json(kept);
			}
			case "GET /api/v1/words":
				return Response.json(vocabulary);
			case "GET /api/v1/libraries": {
				const order = libraryOrders.get(token as string) ?? [];
				const rank = (id: string) =>
					order.includes(id) ? order.indexOf(id) : order.length;
				return Response.json({
					items: libraries.items.toSorted((a, b) => rank(a.id) - rank(b.id)),
				});
			}
			case "PUT /api/v1/profile/library-order": {
				const body = (await request.json()) as Schemas["LibraryOrder"];
				libraryOrders.set(token as string, body.library_ids);
				return new Response(null, { status: 204 });
			}
			case "GET /api/v1/home":
				return Response.json(
					home(
						(preferences.get(token as string) ?? defaults).home,
						Number(url.searchParams.get("limit")) || 20,
					),
				);
			case "GET /api/v1/events": {
				let mine: ReadableStreamDefaultController<string>;
				const body = new ReadableStream<string>({
					start(controller) {
						mine = controller;
						feeds.add(controller);
						controller.enqueue(
							`event: hello\ndata: ${JSON.stringify({ scans: [] })}\n\n`,
						);
					},
					cancel() {
						feeds.delete(mine);
					},
				});
				return new Response(body, {
					headers: { "content-type": "text/event-stream" },
				});
			}
			case "GET /api/v1/playlists":
				return Response.json({
					items: playlists.map(({ items, ...p }) => ({
						...p,
						entries: items.length,
					})),
				} satisfies Schemas["PlaylistList"]);
			case "POST /api/v1/playlists": {
				const body: Schemas["AddPlaylist"] = await json(request);
				const id = `pl-${playlists.length + 1}`;
				playlists.push({
					id,
					name: body.name,
					entries: 0,
					duration_ms: 0,
					updated_at: "2026-10-05T20:00:00Z",
					items: body.item_ids ?? [],
				});
				return Response.json({ id } satisfies Schemas["Created"], {
					status: 201,
				});
			}
			case "GET /api/v1/downloads":
				return Response.json({
					items: downloads,
				} satisfies Schemas["DownloadList"]);
			case "POST /api/v1/downloads": {
				const body: Schemas["DownloadRequest"] = await json(request);
				// As the server, a copy in several files is asked for a file at a time.
				if (body.version_id === "v-hd" && !body.part_id) {
					return Response.json(
						{
							title: "Bad Request",
							status: 400,
							code: "invalid_body",
							detail: "part_id names which of the copy's 2 files",
						},
						{ status: 400 },
					);
				}
				const of = byID(body.title_id);
				const d: Schemas["Download"] = {
					id: `d-${downloads.length + 1}`,
					title_id: body.title_id,
					title: of?.title ?? "",
					show: of?.show?.title,
					part_id: body.part_id ?? "part-1",
					part_index: body.part_id === "p-hd2" ? 1 : 0,
					parts: body.version_id === "v-hd" ? 2 : 1,
					device_id: "device-1",
					method: body.max_bitrate_kbps >= 1_000_000 ? "direct" : "transcode",
					max_bitrate_kbps: body.max_bitrate_kbps,
					state: "ready",
					progress: 1,
					size_bytes: 1_500_000_000,
					url: "/api/v1/parts/part-1/stream?exp=1&sig=x",
					created_at: "2026-10-05T20:00:00Z",
				};
				downloads.unshift(d);
				return Response.json(d);
			}
			case "GET /api/v1/history":
				return Response.json({
					items: [
						{
							id: "h-1",
							profile_id: me.id,
							title: card(byID("t-ep") as Schemas["Card"]),
							method: "direct",
							started_at: "2026-10-04T20:00:00Z",
							stopped_at: "2026-10-04T20:10:00Z",
							position_ms: 600_000,
						},
					],
					offset: 0,
					total: 1,
				} satisfies Schemas["HistoryEntryPage"]);
			case "GET /api/v1/search": {
				const q = (url.searchParams.get("q") ?? "").toLowerCase();
				const found = everything().filter((c) =>
					c.title.toLowerCase().includes(q),
				);
				const people = "ada lane".includes(q)
					? [{ id: person, name: "Ada Lane" }]
					: [];
				return Response.json({
					titles: {
						items: found.slice(0, 20).map(card),
						offset: 0,
						total: found.length,
					},
					people: { items: people, offset: 0, total: people.length },
				} satisfies Schemas["Search"]);
			}
			case `GET /api/v1/people/${person}`:
				return Response.json({
					id: person,
					name: "Ada Lane",
					biography: "Broadcaster.",
					born: "1970-01-02",
					credits: [
						{
							...card(films.find((f) => f.id === "t-film") as Schemas["Card"]),
							credit: "actor",
							role: "Host",
						},
						{
							...card(films.find((f) => f.id === "t-film") as Schemas["Card"]),
							credit: "director",
						},
					],
				} satisfies Schemas["Person"]);
			case "GET /api/v1/auth/devices":
				return Response.json({
					items: [
						{
							id: "d-this",
							device: "Chrome on macOS",
							client: "Photon Web",
							profile: me.name,
							signed_in_at: "2026-10-01T20:00:00Z",
							last_seen_at: "2026-10-05T20:00:00Z",
							this_device: true,
						},
						{
							id: "d-tv",
							device: "Living Room",
							client: "Photon for tvOS",
							profile: "Kids",
							signed_in_at: "2026-09-01T20:00:00Z",
							last_seen_at: "2026-10-04T20:00:00Z",
							this_device: false,
						},
					],
				} satisfies Schemas["DeviceListingList"]);
			case "DELETE /api/v1/auth/devices/d-tv":
				return new Response(null, { status: 204 });
			case "POST /api/v1/auth/pairings/approve": {
				const body = (await request.json()) as Schemas["Approval"];
				if (body.user_code.replace(/[- ]/g, "").toUpperCase() !== "BCDFGHJK") {
					return problem(404, "pairing_not_found", "Not Found");
				}
				return Response.json({
					device: "Living Room",
					client: "Photon for tvOS",
				} satisfies Schemas["Device"]);
			}
		}
		const answered = (await admin(request, url, me)) ?? adminTitles(route);
		if (answered) return answered;
		const parts = url.pathname.split("/").slice(3);
		const [kind, id, sub, entry, leaf] = parts;
		const path = `${request.method} ${kind}/${sub ?? ""}`;
		switch (path) {
			case "GET home/": {
				const row = homeRows().find((r) => r.kind === id && !r.library);
				if (!row) return problem(404, "not_found", "Not Found");
				const items = row.items;
				const offset = Number(url.searchParams.get("offset") ?? 0);
				const limit = Number(url.searchParams.get("limit") ?? 50);
				return Response.json({
					items: items.slice(offset, offset + limit).map(card),
					offset,
					total: items.length,
				} satisfies Schemas["CardPage"]);
			}
			case "GET libraries/titles": {
				const pool = byTitle(id === "l-shows" ? shows : films);
				const genre = url.searchParams.get("genre");
				const sorted =
					url.searchParams.get("order") === "desc" ? [...pool].reverse() : pool;
				const items = genre ? sorted.filter((f) => f.id === "t-film") : sorted;
				const offset = Number(url.searchParams.get("offset") ?? 0);
				const limit = Number(url.searchParams.get("limit") ?? 50);
				return Response.json({
					items: items.slice(offset, offset + limit).map(card),
					offset,
					total: items.length,
				} satisfies Schemas["CardPage"]);
			}
			case "GET libraries/letters": {
				const pool = byTitle(id === "l-shows" ? shows : films);
				const counts = new Map<string, number>();
				for (const f of pool) {
					const l = f.title[0].toUpperCase();
					counts.set(l, (counts.get(l) ?? 0) + 1);
				}
				return Response.json({
					items: [...counts].map(([letter, count]) => ({ letter, count })),
				} satisfies Schemas["LetterList"]);
			}
			case "GET libraries/facets":
				return Response.json(facets);
			case "GET libraries/collections":
				return Response.json({
					items: id === "l-films" ? [collection] : [],
					offset: 0,
					total: id === "l-films" ? 1 : 0,
				} satisfies Schemas["CardPage"]);
			case "GET titles/": {
				const out = page(id);
				return out
					? Response.json(out)
					: problem(404, "not_found", "Not Found");
			}
			case "GET titles/members":
				return Response.json({
					items: [
						card(films.find((f) => f.id === "t-film") as Schemas["Card"]),
					],
				});
			case "GET titles/similar":
				return Response.json({ items: films.slice(1, 26).map(card) });
			case "GET titles/next":
				return Response.json(card(episodes[0]));
			case "PUT titles/watched":
			case "DELETE titles/watched":
			case "PUT titles/favourite":
			case "DELETE titles/favourite":
			case "PUT titles/watchlist":
			case "DELETE titles/watchlist":
			case "DELETE titles/progress": {
				const now = request.method === "PUT" ? "2026-10-05T20:00:00Z" : null;
				const state = { ...states.get(id) };
				if (sub === "watched") {
					state.watched_at = now;
					state.position_ms = 0;
				} else if (sub === "favourite") state.favourite_at = now;
				else if (sub === "watchlist") state.watchlisted_at = now;
				else state.position_ms = 0;
				states.set(id, state);
				return none();
			}
			case "PATCH playlists/":
			case "DELETE playlists/": {
				const list = playlists.find((p) => p.id === id);
				if (!list) return problem(404, "not_found", "Not Found");
				if (request.method === "DELETE") {
					playlists = playlists.filter((p) => p !== list);
				} else list.name = ((await json(request)) as Schemas["Name"]).name;
				return none();
			}
			case "GET playlists/entries":
			case "POST playlists/entries":
			case "PUT playlists/entries":
			case "DELETE playlists/entries": {
				const list = playlists.find((p) => p.id === id);
				if (!list) return problem(404, "not_found", "Not Found");
				if (request.method === "POST") {
					list.items.push(
						...((await json(request)) as Schemas["ItemIDs"]).item_ids,
					);
					return none();
				}
				const at = list.items.findIndex((_, i) => `e-${i}` === entry);
				if (request.method === "DELETE") list.items.splice(at, 1);
				if (request.method === "PUT" && leaf === "position") {
					const [moved] = list.items.splice(at, 1);
					list.items.splice(
						((await json(request)) as Schemas["Move"]).position,
						0,
						moved,
					);
				}
				if (request.method !== "GET") return none();
				return Response.json({
					items: list.items.map((item, i) => ({
						...card(byID(item) as Schemas["Card"]),
						entry_id: `e-${i}`,
					})),
					offset: 0,
					total: list.items.length,
				} satisfies Schemas["EntryPage"]);
			}
		}
		return problem(404, "not_found", "Not Found");
	},
});

console.log(`mock server on ${server_.url}`);
