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

// "1:04:09", "4:09": a position on a title's timeline.
export function timecode(ms: number): string {
	const s = Math.floor(ms / 1000);
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

export const ratingSites: Record<Schemas["RatingSite"], string> = {
	imdb: "IMDb",
	tmdb: "TMDB",
	rotten_tomatoes: "Rotten Tomatoes",
	rotten_tomatoes_audience: "RT Audience",
	metacritic: "Metacritic",
	letterboxd: "Letterboxd",
	trakt: "Trakt",
};

// A score as its site prints it: IMDb and TMDB out of ten, Letterboxd out of
// five, the others as a percentage. The server keeps every score from 0 to 100.
export function score(site: Schemas["RatingSite"], value: number): string {
	switch (site) {
		case "imdb":
		case "tmdb":
		case "trakt":
			return (value / 10).toFixed(1);
		case "letterboxd":
			return (value / 20).toFixed(1);
		case "rotten_tomatoes":
		case "rotten_tomatoes_audience":
			return `${Math.round(value)}%`;
		case "metacritic":
			return String(Math.round(value));
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

export function bitrate(kbps: number): string {
	return kbps >= 1000 ? `${(kbps / 1000).toFixed(1)} Mbps` : `${kbps} kbps`;
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
	const lines =
		stream.width && stream.height
			? resolutionOf(stream.width, stream.height)
			: "";
	const range =
		stream.range && stream.range !== "sdr" ? rangeName(stream.range) : "";
	return [lines, stream.codec.toUpperCase(), range].filter(Boolean).join(" ");
}

// A frame named by its width, so a scope master is not "1600p".
function resolutionOf(width: number, height: number): string {
	if (width >= 3200 || height >= 1800) return "4K";
	if (width >= 1700 || height >= 1000) return "1080p";
	if (width >= 1100 || height >= 650) return "720p";
	return "SD";
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
