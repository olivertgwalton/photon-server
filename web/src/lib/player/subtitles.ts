import type { components } from "#lib/api/schema.js";
import { language } from "./words.js";

type Schemas = components["schemas"];

// A subtitle the reader can choose: a track inside the copy, by its stream
// index, or a file beside it, by its place among the copy's files.
export type Choice = {
	key: string;
	label: string;
	codec: string;
	stream?: number;
	file?: number;
	// Its place among an HLS playlist's subtitles: every text track inside
	// the copy, then every text file beside it. A picture has none.
	rendition?: number;
};

// FFmpeg's text subtitle codecs, which HLS carries as WebVTT; the server's
// hls.TextSubtitle.
const text = new Set([
	"subrip",
	"ass",
	"ssa",
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
		});
	}
	// A picture beside the copy is never drawn: only a track inside it is.
	(version.subtitles ?? []).forEach((f, i) => {
		if (!text.has(f.codec)) return;
		out.push({
			key: `f${i}`,
			label: name(f),
			codec: f.codec,
			file: i,
			rendition: rendition++,
		});
	});
	return out;
}

// What a play asks for so the choice can be shown: a picture drawn into the
// video, and anything a browser cannot read from a file played as it is
// (a track inside it, an ASS file) carried in HLS as WebVTT.
export function wants(choice: Choice | undefined): {
	subtitle_stream?: number;
	viaHLS: boolean;
} {
	if (!choice) return { viaHLS: false };
	if (choice.rendition === undefined) {
		return { subtitle_stream: choice.stream, viaHLS: false };
	}
	const beside = choice.file !== undefined && drawnBeside.has(choice.codec);
	return { subtitle_stream: choice.stream, viaHLS: !beside };
}

// Whether showing a choice needs a new playback, rather than a track switched
// on in the one playing.
export function needsReplay(
	choice: Choice | undefined,
	playback: Schemas["Playback"],
): boolean {
	const burned = playback.video?.burned_subtitle ?? undefined;
	if (choice?.rendition === undefined && choice) {
		return burned !== choice.stream;
	}
	if (burned !== undefined) return true;
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
