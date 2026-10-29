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
	const films = count(c.movies, "film");
	const shows = [
		count(c.shows, "show"),
		count(c.seasons, "season"),
		count(c.episodes, "episode"),
	];
	switch (kind) {
		case "movies":
			return films;
		case "shows":
			return shows.join(" · ");
		case "mixed":
			return [films, ...shows].join(" · ");
	}
}

// "1:04:09", "4:09": a position on a title's timeline.
export function timecode(ms: number): string {
	const s = Math.max(0, Math.floor(ms / 1000));
	const h = Math.floor(s / 3600);
	const mm = String(Math.floor((s % 3600) / 60));
	const ss = String(s % 60).padStart(2, "0");
	return h ? `${h}:${mm.padStart(2, "0")}:${ss}` : `${mm}:${ss}`;
}

// "Resume 01:04:09", "Resume 00:04:09": where Play goes on from, as the apps'
// Play names it, the hours always written.
export function resumeLabel(ms: number): string {
	const s = Math.max(0, Math.floor(ms / 1000));
	const clock = [Math.floor(s / 3600), Math.floor((s % 3600) / 60), s % 60]
		.map((n) => String(n).padStart(2, "0"))
		.join(":");
	return `Resume ${clock}`;
}

// "S1, E2", "S1, E2–E3" for a double episode, "E5" where the season is not known. The
// comma is the Photon apps' own spelling, which a card reads beside.
export function episodeLabel(
	season: number | null | undefined,
	episode: number | null | undefined,
	end?: number | null,
): string {
	if (episode == null) return season == null ? "" : `S${season}`;
	const span =
		end != null && end > episode ? `E${episode}–E${end}` : `E${episode}`;
	return season == null ? span : `S${season}, ${span}`;
}

// "Small Show: Second": a title named with its show, where it has one, as a
// menu or a dialog names what it acts on.
export function titleWithShow(t: {
	title: string;
	show?: { title: string } | null;
}): string {
	return t.show ? `${t.show.title}: ${t.title}` : t.title;
}

// "S1, E2 · Small Show": what a card says under an episode's own name, as the
// apps' cards do: where it is in its show, and the show. A season's is its show.
export function showLine(t: {
	kind: Schemas["ItemKind"];
	show?: { title: string } | null;
	season_number?: number | null;
	episode_number?: number | null;
	episode_end?: number | null;
}): string {
	const at =
		t.kind === "episode" &&
		episodeLabel(t.season_number, t.episode_number, t.episode_end);
	return [at, t.show?.title].filter(Boolean).join(" · ");
}

// "Small Show S1, E2 · Second": an episode as one line, by its show and place
// in it; anything else by its own title.
export function fullTitle(
	t: {
		kind: Schemas["ItemKind"];
		title: string;
		season_number?: number | null;
		episode_number?: number | null;
		episode_end?: number | null;
	},
	show: string | undefined,
): string {
	if (t.kind !== "episode" || !show) return t.title;
	const at = episodeLabel(t.season_number, t.episode_number, t.episode_end);
	return `${[show, at].filter(Boolean).join(" ")} · ${t.title}`;
}

// The mark a score is drawn with, as the Photon apps draw it: Rotten Tomatoes'
// fresh or rotten by its own line at 60%.
export type RatingMark =
	| "imdb"
	| "tmdb"
	| "tomatometer-fresh"
	| "tomatometer-rotten"
	| "popcorn-upright"
	| "popcorn-spilled"
	| "anilist"
	| "anidb";

export function ratingMark(
	site: Schemas["RatingSite"],
	value: number,
): RatingMark {
	switch (site) {
		case "imdb":
		case "tmdb":
		case "anilist":
		case "anidb":
			return site;
		case "rotten_tomatoes":
			return value >= 60 ? "tomatometer-fresh" : "tomatometer-rotten";
		case "rotten_tomatoes_audience":
			return value >= 60 ? "popcorn-upright" : "popcorn-spilled";
	}
}

// A score as its site prints it: IMDb, TMDB and AniDB out of ten, Rotten
// Tomatoes and AniList as a percentage. The server keeps every score from 0 to
// 100.
export function score(site: Schemas["RatingSite"], value: number): string {
	switch (site) {
		case "imdb":
		case "tmdb":
		case "anidb":
			return (value / 10).toFixed(1);
		case "rotten_tomatoes":
		case "rotten_tomatoes_audience":
		case "anilist":
			return `${Math.round(value)}%`;
	}
}

const units = ["byte", "kilobyte", "megabyte", "gigabyte", "terabyte"];

export function bytes(n: number): string {
	let i = 0;
	while (n >= 1000 && i < units.length - 1) {
		n /= 1000;
		i++;
	}
	return new Intl.NumberFormat(undefined, {
		style: "unit",
		unit: units[i],
		maximumFractionDigits: i && n < 10 ? 1 : 0,
	}).format(n);
}

const kilobits = new Intl.NumberFormat(undefined, {
	style: "unit",
	unit: "kilobit-per-second",
});
const megabits = new Intl.NumberFormat(undefined, {
	style: "unit",
	unit: "megabit-per-second",
	maximumFractionDigits: 1,
});

// "8.5 Mb/s", "40 Mb/s", "420 kb/s".
export function bitrate(kbps: number): string {
	return kbps >= 1000 ? megabits.format(kbps / 1000) : kilobits.format(kbps);
}

// "Mb/s" alone, for a field that takes a number of them.
export const megabitUnit = megabits
	.formatToParts(0)
	.find((p) => p.type === "unit")?.value;

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

const regions = new Intl.DisplayNames(undefined, { type: "region" });

export function country(code: string): string {
	try {
		return regions.of(code) ?? code;
	} catch {
		return code;
	}
}

// Languages or countries as choices, named in the reader's own language and
// in its order, as Plex's and Jellyfin's settings list them.
export function localeOptions(
	codes: Iterable<string>,
	name: (code: string) => string = language,
): { value: string; label: string }[] {
	return [...codes]
		.map((value) => ({ value, label: name(value) }))
		.toSorted((a, b) => a.label.localeCompare(b.label));
}

// The version asked for by id, else the one the server plays when none is: the
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

// Where a title is played. The player reads the version, the tracks and where to
// start from the address; with no `t` it resumes where the profile stopped.
export function playHref(
	id: string,
	choice: {
		version?: string;
		audio?: number;
		subtitle?: number | "off";
		t?: number;
	} = {},
): string {
	const query = new URLSearchParams();
	if (choice.version) query.set("version", choice.version);
	if (choice.audio != null) query.set("audio", String(choice.audio));
	if (choice.subtitle != null) query.set("subtitle", String(choice.subtitle));
	if (choice.t != null) query.set("t", String(choice.t));
	const q = query.toString();
	return `/play/${id}${q ? `?${q}` : ""}`;
}
