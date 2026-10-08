import type { components } from "#lib/api/schema.js";

type Schemas = components["schemas"];

// A subtitle the reader can choose: a track inside the copy, by its stream
// index, or a file beside it, by its place among the copy's files.
type Choice = {
	key: string;
	label: string;
	codec: string;
	kind?: Schemas["SubtitleKind"];
	stream?: number;
	file?: number;
	// A file's id, which a play names it by.
	id?: string;
	// Its place among an HLS playlist's subtitles: every plain text track
	// inside the copy, then every plain text file beside it. A picture or
	// styled text has none.
	rendition?: number;
};

// What a browser reads from a file played as it is.
const drawnBeside = new Set(["subrip", "webvtt"]);

export function choices(version: Schemas["VersionPage"]): Choice[] {
	const out: Choice[] = [];
	let rendition = 0;
	for (const s of version.streams) {
		if (s.kind !== "subtitle") continue;
		out.push({
			key: `s${s.index}`,
			label: s.display_title,
			codec: s.codec,
			kind: s.subtitle_kind,
			stream: s.index,
			rendition: s.subtitle_kind === "text" ? rendition++ : undefined,
		});
	}
	// A picture beside the copy is never drawn: only a track inside it is.
	(version.subtitles ?? []).forEach((f, i) => {
		if (f.kind === "picture") return;
		out.push({
			key: `f${i}`,
			label: f.display_title,
			codec: f.codec,
			kind: f.kind,
			file: i,
			id: f.id,
			rendition: f.kind === "text" ? rendition++ : undefined,
		});
	});
	return out;
}

// The subtitle a play starts with, by its choice's key: the one the address
// asks for, else the one the server chose for this profile, else none.
export function startingSubtitle(
	version: Schemas["VersionPage"],
	asked: number | "off" | undefined,
): string | undefined {
	if (asked === "off") return undefined;
	if (asked !== undefined) return `s${asked}`;
	const file = (version.subtitles ?? []).findIndex(
		(f) => f.id === version.default_subtitle_file,
	);
	if (file >= 0) return `f${file}`;
	const stream = version.default_subtitle_stream;
	return stream == null ? undefined : `s${stream}`;
}

// What a play asks for so the choice can be shown: a picture drawn into the
// video, styled text handed to the browser where it draws it and drawn in
// where it does not, and plain text a browser cannot read from a file played
// as it is (a track inside it) carried in HLS as WebVTT.
export function wants(choice: Choice | undefined): {
	subtitle_stream?: number;
	subtitle_file?: string;
	viaHLS: boolean;
} {
	if (!choice) return { viaHLS: false };
	const asked = { subtitle_stream: choice.stream, subtitle_file: choice.id };
	if (choice.rendition === undefined) return { ...asked, viaHLS: false };
	const beside = choice.file !== undefined && drawnBeside.has(choice.codec);
	return { ...asked, viaHLS: !beside };
}

// The subtitle a playback hands the browser to draw beside the video for a
// choice: a file beside the copy, or a styled track read out of it.
export function beside(
	choice: Choice | undefined,
	playback: Schemas["Playback"],
): Schemas["Subtitle"] | undefined {
	if (!choice) return undefined;
	return playback.subtitles?.find((s) =>
		choice.id === undefined ? s.stream === choice.stream : s.id === choice.id,
	);
}

// Whether showing a choice needs a new playback, rather than a track switched
// on in the one playing.
export function needsReplay(
	choice: Choice | undefined,
	playback: Schemas["Playback"],
): boolean {
	const burned = playback.video?.burned_subtitle ?? undefined;
	const burnedFile = playback.video?.burned_subtitle_file ?? undefined;
	if (choice?.rendition === undefined && choice) {
		if (
			burned === undefined &&
			burnedFile === undefined &&
			beside(choice, playback)
		) {
			return false;
		}
		return choice.id === undefined
			? burned !== choice.stream
			: burnedFile !== choice.id;
	}
	if (burned !== undefined || burnedFile !== undefined) return true;
	return playback.method === "direct" && wants(choice).viaHLS;
}

// A SubRip file as WebVTT, which is all a browser's track reads.
// ponytail: decoded as UTF-8, so a file in another charset reads garbled
// where HLS would convert it; sniff the charset if that bites.
export function webVTT(srt: string): string {
	const body = srt
		.replace(/^\uFEFF/, "")
		.replace(/\r\n?/g, "\n")
		.replace(/(\d\d:\d\d:\d\d),(\d\d\d)/g, "$1.$2");
	return `WEBVTT\n\n${body}`;
}
