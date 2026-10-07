import type { components } from "#lib/api/schema.js";
import { language } from "#lib/format.js";

type Schemas = components["schemas"];

// A subtitle the reader can choose: a track inside the copy, by its stream
// index, or a file beside it, by its place among the copy's files.
type Choice = {
	key: string;
	label: string;
	codec: string;
	stream?: number;
	file?: number;
	// A file's id, which a play names it by.
	id?: string;
	// Its place among an HLS playlist's subtitles: every plain text track
	// inside the copy, then every plain text file beside it. A picture or
	// styled text has none.
	rendition?: number;
	language?: string;
	forced?: boolean;
	default?: boolean;
};

// FFmpeg's plain text subtitle codecs, which HLS carries as WebVTT; the
// server's hls.TextSubtitle.
const text = new Set([
	"subrip",
	"webvtt",
	"mov_text",
	"text",
	"sami",
	"microdvd",
	"subviewer",
	"realtext",
]);

// What a browser reads from a file played as it is.
const drawnBeside = new Set(["subrip", "webvtt"]);

// Styled text, which WebVTT would lose the look of; the server's
// hls.StyledSubtitle.
const styled = new Set(["ass", "ssa"]);

function name(s: {
	title?: string;
	language?: string;
	forced?: boolean;
	hearing_impaired?: boolean;
}): string {
	const base = s.title || language(s.language) || "Unknown";
	const marks = [s.forced && "Forced", s.hearing_impaired && "SDH"].filter(
		Boolean,
	);
	return marks.length ? `${base} (${marks.join(", ")})` : base;
}

export function choices(version: Schemas["VersionPage"]): Choice[] {
	const out: Choice[] = [];
	let rendition = 0;
	for (const s of version.streams) {
		if (s.kind !== "subtitle") continue;
		out.push({
			key: `s${s.index}`,
			label: name(s),
			codec: s.codec,
			stream: s.index,
			rendition: text.has(s.codec) ? rendition++ : undefined,
			language: s.language,
			forced: s.forced,
			default: s.default,
		});
	}
	// A picture beside the copy is never drawn: only a track inside it is.
	(version.subtitles ?? []).forEach((f, i) => {
		if (!text.has(f.codec) && !styled.has(f.codec)) return;
		out.push({
			key: `f${i}`,
			label: name(f),
			codec: f.codec,
			file: i,
			id: f.id,
			rendition: text.has(f.codec) ? rendition++ : undefined,
			language: f.language,
			forced: f.forced,
			default: f.default,
		});
	});
	return out;
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

// Whether a codec is styled text, which JASSUB draws.
export const isStyled = (codec: string) => styled.has(codec);

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
