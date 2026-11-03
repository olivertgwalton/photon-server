import type { components } from "./api/schema.d.ts";
import type { Listen } from "./player/listen.ts";

type Schemas = components["schemas"];
type Version = Schemas["VersionPage"];

// What one of a book's versions is: an ebook's file, read elsewhere, or an
// audiobook's sound, listened to here.
export type BookFormat = "audiobook" | "epub" | "pdf" | "cbz";

const ebooks = ["epub", "pdf", "cbz"] as const;

export function formatOf(version: Version): BookFormat {
	const ebook = ebooks.find((e) => e === version.container);
	return ebook ?? "audiobook";
}

export const formatNames: Record<BookFormat, string> = {
	audiobook: "Audiobook",
	epub: "EPUB",
	pdf: "PDF",
	cbz: "Comic book",
};

// "Harry Potter, book 7", "Discworld, book 1.5": where a book stands in its
// series.
export function seriesLine(
	series: string | undefined,
	index: number | null | undefined,
): string | undefined {
	if (!series) return undefined;
	return index == null ? series : `${series}, book ${index}`;
}

// An audiobook's chapters: its own, or where it has none and comes in several
// files, one for each file, as Audiobookshelf makes them.
export function chaptersOf(version: Version): Schemas["ChapterRef"][] {
	if (version.chapters?.length) return version.chapters;
	if (version.files.length < 2) return [];
	return version.files.map((f) => ({
		start_ms: f.offset_ms,
		end_ms: f.offset_ms + f.duration_ms,
		title: `Part ${f.index + 1}`,
	}));
}

// An audiobook's version as the audio player plays it, from where the
// listener stopped, or from a chapter.
export function bookListen(
	book: Schemas["TitlePage"],
	version: Version,
	start_ms = book.state?.position_ms ?? 0,
): Listen {
	const cover = book.artwork?.poster?.[0];
	return {
		id: book.id,
		version: version.id,
		title: book.title,
		by: book.author?.title,
		artwork: cover,
		blurhash: cover ? book.blurhashes?.[cover] : undefined,
		duration_ms: version.duration_ms,
		chapters: chaptersOf(version),
		start_ms,
	};
}

// The profile a play of an ebook's version is sent with, to be given its file
// as it is: the server serves an ebook only ever as a download.
export function ebookProfile(version: Version): Schemas["ClientProfile"] {
	return {
		containers: [version.container],
		video: [],
		audio: [],
		max_bitrate_kbps: 0,
		parts: "each",
	};
}
