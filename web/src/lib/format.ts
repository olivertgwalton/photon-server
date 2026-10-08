import type { components } from "./api/schema.js";

type Schemas = components["schemas"];

// "1h 52m", "48m": a running time as a listing prints it.
export function runtime(ms: number): string {
	const minutes = Math.max(1, Math.round(ms / 60_000));
	const h = Math.floor(minutes / 60);
	const m = minutes % 60;
	if (!h) return `${m}m`;
	return m ? `${h}h ${m}m` : `${h}h`;
}

// "1 season", "3 seasons".
export function count(n: number, thing: string): string {
	return `${n.toLocaleString()} ${thing}${n === 1 ? "" : "s"}`;
}

// "1,204 films", "12 shows · 40 seasons · 512 episodes": what a library holds,
// as its kind counts it.
export function holding(
	kind: Schemas["LibraryKind"],
	c: Schemas["Counts"],
): string {
	if (kind === "movies") return count(c.movies, "film");
	return [
		count(c.shows, "show"),
		count(c.seasons, "season"),
		count(c.episodes, "episode"),
	].join(" · ");
}

// "1:04:09", "4:09": a position on a title's timeline.
export function timecode(ms: number): string {
	const s = Math.max(0, Math.floor(ms / 1000));
	const h = Math.floor(s / 3600);
	const mm = String(Math.floor((s % 3600) / 60));
	const ss = String(s % 60).padStart(2, "0");
	return h ? `${h}:${mm.padStart(2, "0")}:${ss}` : `${mm}:${ss}`;
}

// "S1 E2", "S1 E2–E3" for a double episode, "E5" where the season is not known.
export function episodeLabel(
	season: number | null | undefined,
	episode: number | null | undefined,
	end?: number | null,
): string {
	if (episode == null) return season == null ? "" : `S${season}`;
	const span =
		end != null && end > episode ? `E${episode}–E${end}` : `E${episode}`;
	return season == null ? span : `S${season} ${span}`;
}

// "Small Show: Second": a title named with its show, where it has one, as a
// menu or a dialog names what it acts on.
export function titleWithShow(t: {
	title: string;
	show?: { title: string } | null;
}): string {
	return t.show ? `${t.show.title}: ${t.title}` : t.title;
}

// "Small Show S1 E2 · Second": an episode as one line, by its show and place
// in it; anything else by its own title.
export function fullTitle(
	t: {
		title: string;
		season_number?: number | null;
		episode_number?: number | null;
		episode_end?: number | null;
	},
	show: string | undefined,
): string {
	if (!show) return t.title;
	const at = episodeLabel(t.season_number, t.episode_number, t.episode_end);
	return `${[show, at].filter(Boolean).join(" ")} · ${t.title}`;
}

export const playMethods: Record<Schemas["PlayMethod"], string> = {
	direct: "Direct play",
	remux: "Direct stream",
	transcode: "Transcode",
};

export const ratingSites: Record<Schemas["RatingSite"], string> = {
	imdb: "IMDb",
	tmdb: "TMDB",
	rotten_tomatoes: "Rotten Tomatoes",
	rotten_tomatoes_audience: "RT Audience",
};

// The mark a score is drawn with, as the Photon apps draw it: Rotten Tomatoes'
// fresh or rotten by its own line at 60%.
export type RatingMark =
	| "imdb"
	| "tmdb"
	| "tomatometer-fresh"
	| "tomatometer-rotten"
	| "popcorn-upright"
	| "popcorn-spilled";

export function ratingMark(
	site: Schemas["RatingSite"],
	value: number,
): RatingMark {
	switch (site) {
		case "imdb":
		case "tmdb":
			return site;
		case "rotten_tomatoes":
			return value >= 60 ? "tomatometer-fresh" : "tomatometer-rotten";
		case "rotten_tomatoes_audience":
			return value >= 60 ? "popcorn-upright" : "popcorn-spilled";
	}
}

// A score as its site prints it: IMDb and TMDB out of ten, Rotten Tomatoes as a
// percentage. The server keeps every score from 0 to 100.
export function score(site: Schemas["RatingSite"], value: number): string {
	switch (site) {
		case "imdb":
		case "tmdb":
			return (value / 10).toFixed(1);
		case "rotten_tomatoes":
		case "rotten_tomatoes_audience":
			return `${Math.round(value)}%`;
	}
}

const units = ["B", "KB", "MB", "GB", "TB"];

export function bytes(n: number): string {
	let i = 0;
	while (n >= 1000 && i < units.length - 1) {
		n /= 1000;
		i++;
	}
	return `${n.toFixed(i && n < 10 ? 1 : 0)} ${units[i]}`;
}

// "8.5 Mbps", "40 Mbps", "420 kbps".
export function bitrate(kbps: number): string {
	return kbps >= 1000
		? `${Number((kbps / 1000).toFixed(1))} Mbps`
		: `${kbps} kbps`;
}

const languages = new Intl.DisplayNames(undefined, { type: "language" });

// A stream's language by name; a code the browser does not know stays a code.
export function language(code: string | undefined): string {
	if (!code || code === "und") return "";
	try {
		return languages.of(code) ?? code;
	} catch {
		return code;
	}
}

const ranges: Record<Schemas["Range"], string> = {
	sdr: "SDR",
	hlg: "HLG",
	hdr10: "HDR10",
	hdr10plus: "HDR10+",
	dv: "Dolby Vision",
};

export function rangeName(range: Schemas["Range"]): string {
	return ranges[range];
}

export const resolutionNames: Record<Schemas["Resolution"], string> = {
	sd: "SD",
	"720p": "720p",
	"1080p": "1080p",
	"4k": "4K",
};

// "2160p HEVC Dolby Vision": what a copy's picture is, for choosing between copies.
export function videoName(stream: Schemas["StreamPage"] | undefined): string {
	if (!stream) return "";
	const lines = stream.resolution ? resolutionNames[stream.resolution] : "";
	const range =
		stream.range && stream.range !== "sdr" ? rangeName(stream.range) : "";
	return [lines, stream.codec.toUpperCase(), range].filter(Boolean).join(" ");
}

// The copy asked for by id, else the one the server plays when none is: the
// first with its files on disk.
export function onDisk(
	versions: Schemas["VersionPage"][] | undefined,
	id?: string,
): Schemas["VersionPage"] | undefined {
	return (
		versions?.find((v) => v.id === id) ??
		versions?.find((v) => !v.missing_since)
	);
}

// A copy as a choice names it: its label or edition and its picture, and
// whether its files are gone.
export function versionName(v: Schemas["VersionPage"]): string {
	const picture = videoName(v.streams.find((s) => s.kind === "video"));
	const name = v.label ?? v.edition ?? "";
	// A label is often the picture's own name ("4K"), which says it once.
	const said =
		!name || picture.startsWith(name)
			? picture
			: [name, picture].filter(Boolean).join(" · ");
	return `${said || "Version"}${v.missing_since ? " (missing)" : ""}`;
}

// "English · AC3 5.1 · Commentary": an audio or subtitle track in a menu.
export function trackName(stream: Schemas["StreamPage"]): string {
	const channels =
		stream.channel_layout ??
		(stream.channels ? `${stream.channels} ch` : undefined);
	return [
		language(stream.language) || stream.title || `Track ${stream.index}`,
		stream.kind === "audio"
			? [stream.codec.toUpperCase(), channels].filter(Boolean).join(" ")
			: stream.codec.toUpperCase(),
		stream.forced && "Forced",
		stream.hearing_impaired && "SDH",
		stream.commentary && "Commentary",
	]
		.filter(Boolean)
		.join(" · ");
}

// Where a title is played. The player reads the copy, the tracks and where to
// start from the address; with no `t` it resumes where the profile stopped.
export function playHref(
	id: string,
	choice: {
		version?: string;
		audio?: number;
		subtitle?: number | "off";
		t?: number;
		playlist?: string;
		shuffle?: boolean;
	} = {},
): string {
	const query = new URLSearchParams();
	if (choice.version) query.set("version", choice.version);
	if (choice.audio != null) query.set("audio", String(choice.audio));
	if (choice.subtitle != null) query.set("subtitle", String(choice.subtitle));
	if (choice.t != null) query.set("t", String(choice.t));
	if (choice.playlist) query.set("playlist", choice.playlist);
	if (choice.shuffle) query.set("shuffle", "1");
	const q = query.toString();
	return `/play/${id}${q ? `?${q}` : ""}`;
}
