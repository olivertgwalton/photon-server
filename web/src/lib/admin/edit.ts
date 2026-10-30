import type { components } from "#lib/api/schema.d.ts";
import { text } from "#lib/form.ts";
import { parseClock } from "./words.ts";

type Schemas = components["schemas"];
type Field = Schemas["Field"];
type Kind = Schemas["ItemKind"];

// What an admin can do to a title, as Plex's and Jellyfin's offer: a film or
// show is matched, and all but a collection or an extra refreshed.
export function matchable(kind: Kind): boolean {
	return kind === "movie" || kind === "show";
}

export function refreshable(kind: Kind): boolean {
	return kind !== "collection" && kind !== "extra";
}

// The fields an admin edits, as the title page says them now.
export function fieldsOf(t: Schemas["TitlePage"]): Record<Field, string> {
	return {
		title: t.title,
		sort_title: "",
		original_title: t.original_title ?? "",
		overview: t.overview ?? "",
		tagline: t.tagline ?? "",
		certificate: t.qualified_certificate ?? "",
		release_date: t.release_date ?? "",
		year: t.year ? String(t.year) : "",
		genres: (t.genres ?? []).join(", "),
		studios: (t.studios ?? []).join(", "),
		airs_before_season: t.airs_before_season
			? String(t.airs_before_season)
			: "",
		airs_before_episode: t.airs_before_episode
			? String(t.airs_before_episode)
			: "",
		airs_after_season: t.airs_after_season ? String(t.airs_after_season) : "",
	};
}

function list(text: string) {
	return text
		.split(",")
		.map((s) => s.trim())
		.filter(Boolean);
}

// An edit form as the change to send: each field that differs from what the
// title says, and the fields locked as they are. A field cannot be emptied, only
// unlocked to take its sources' again, so emptying one is refused.
export function editOf(
	form: FormData,
	t: Schemas["TitlePage"],
): Schemas["Edit"] | string {
	const now = fieldsOf(t);
	const edit: Schemas["Edit"] = {};
	for (const field of Object.keys(now) as Field[]) {
		const value = text(form, field);
		if (value === now[field]) continue;
		if (!value) {
			return "A field can't be emptied. Unlock it to take the sources' again instead.";
		}
		switch (field) {
			case "year":
			case "airs_before_season":
			case "airs_before_episode":
			case "airs_after_season":
				edit[field] = Number(value);
				break;
			case "genres":
			case "studios":
				edit[field] = list(value);
				break;
			default:
				edit[field] = value;
		}
	}
	const locked = form.getAll("locked").map(String) as Field[];
	if (locked.length) edit.locked = locked;
	return edit;
}

// A markers form, row by row, as the markers to say; a time that does not read
// as one is refused by its row.
export function markersOf(form: FormData): Schemas["Markers"] | string {
	const kinds = form.getAll("kind").map(String) as Schemas["MarkerKind"][];
	const starts = form.getAll("start").map(String);
	const ends = form.getAll("end").map(String);
	const markers: Schemas["Marker"][] = [];
	for (const [i, kind] of kinds.entries()) {
		const start_ms = parseClock(starts[i] ?? "");
		const end_ms = parseClock(ends[i] ?? "");
		if (start_ms === undefined || end_ms === undefined) {
			return `Row ${i + 1}'s times read as minutes:seconds, like 1:30.`;
		}
		if (end_ms <= start_ms) return `Row ${i + 1} ends before it starts.`;
		markers.push({ kind, start_ms, end_ms });
	}
	const absent = form.getAll("absent").map((value) => {
		const [kind, part] = String(value).split(":");
		return { kind: kind as Schemas["MarkerKind"], part: Number(part) };
	});
	return absent.length ? { markers, absent } : { markers };
}
