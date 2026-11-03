import type { components } from "#lib/api/schema.d.ts";
import { textAsTyped } from "#lib/form.ts";

type Schemas = components["schemas"];
type Ranked = Schemas["RankedSource"];

type Fetcher = "metadata" | "images" | "lyrics";

// What a library ranks sources for, in the order its settings ask them.
const fetchers: Fetcher[] = ["metadata", "images", "lyrics"];

// The kinds of item a provider may be ranked for to fetch f.
function kindsOf(
	p: Schemas["MetadataProvider"],
	f: Fetcher,
): Schemas["ItemKind"][] {
	switch (f) {
		case "metadata":
			return p.metadata_kinds;
		case "images":
			return p.image_kinds;
		case "lyrics":
			return p.lyrics_kinds;
	}
}

// The sources a library may rank to fetch f for an item of a kind: the
// built-ins the server says it may that are no provider, named as Jellyfin
// names its own ("Nfo"), and each provider that says it may be.
export function offeredSources(
	providers: Schemas["MetadataProvider"][],
	fetchable: Schemas["FieldSource"][],
	f: Fetcher,
	kind: Schemas["ItemKind"],
) {
	return [
		...fetchable
			.filter((id) => !providers.some((p) => p.id === id))
			.map((id) => ({
				id,
				name: id.charAt(0).toUpperCase() + id.slice(1),
				ready: true,
			})),
		...providers
			.filter((p) => kindsOf(p, f).includes(kind))
			.map((p) => ({ id: p.id, name: p.name, ready: p.ready })),
	];
}

// Unticked sources at the foot of a list say nothing a list without them
// does not.
function said(list: Ranked[]) {
	const last = list.findLastIndex((r) => r.enabled);
	return JSON.stringify(list.slice(0, last + 1));
}

function same(a: readonly string[], b: readonly string[]) {
	return a.length === b.length && a.every((x, i) => x === b[i]);
}

// Whether a library of a kind holds films and shows, whose videos, subtitles,
// previews, ratings and names in other languages a library's settings say
// what to do with; music and books have none of them.
export function playsVideo(kind: Schemas["LibraryKind"]): boolean {
	return kind !== "music" && kind !== "books";
}

// A library form's fields as a change to the library. The sources and the
// extras are sent only where they differ from what it has, since sending them
// identifies every title in it again. A music or books library's form asks
// nothing of what only video has, which is left as it is.
export function libraryChange(
	form: FormData,
	current: Schemas["AdminLibrary"],
): Schemas["LibraryChange"] {
	const extras = form
		.getAll("remote_extras")
		.map(String) as Schemas["ExtraKind"][];
	const change: Schemas["LibraryChange"] = {
		name: textAsTyped(form, "name"),
		// A remote library has no folder to watch, to delete from, or to forget
		// versions of, and the form asks none of those of one.
		monitor: (form.get("monitor") ?? undefined) as
			| Schemas["Monitor"]
			| undefined,
		deletion: (form.get("deletion") ?? undefined) as
			| Schemas["MediaDeletion"]
			| undefined,
		missing_versions: (form.get("missing_versions") ?? undefined) as
			| Schemas["MissingVersion"]
			| undefined,
		refresh_days: Number(form.get("refresh_days")),
	};
	if (playsVideo(current.kind)) {
		Object.assign(change, {
			previews: String(form.get("previews")) as Schemas["PreviewLevel"],
			markers: String(form.get("markers")) as Schemas["MarkerDetection"],
			keyframes: String(form.get("keyframes")) as Schemas["KeyframeMode"],
			themes: String(form.get("themes")) as Schemas["ThemeLookup"],
			metadata_language: localeChoice(form.get("metadata_language")),
			certification_country: localeChoice(form.get("certification_country")),
			artwork_language: String(
				form.get("artwork_language"),
			) as Schemas["ArtworkLanguage"],
			title_language: String(
				form.get("title_language"),
			) as Schemas["TitleLanguage"],
			box_sets: String(form.get("box_sets")) as Schemas["BoxSetMode"],
			subtitle_languages: form.getAll("subtitle_languages").map(String),
			subtitle_match: String(
				form.get("subtitle_match"),
			) as Schemas["SubtitleMatch"],
		} satisfies Schemas["LibraryChange"]);
	}
	const sources: Schemas["KindSourcesChange"][] = [];
	for (const has of current.sources) {
		const asked: Schemas["KindSourcesChange"] = { kind: has.kind };
		for (const f of fetchers) {
			const given = form.get(`sources-${has.kind}-${f}`);
			if (given === null) continue;
			const list = JSON.parse(String(given)) as Ranked[];
			if (said(list) !== said(has[f])) asked[f] = list;
		}
		if (fetchers.some((f) => asked[f])) sources.push(asked);
	}
	if (sources.length) change.sources = sources;
	if (!same([...extras].sort(), [...current.remote_extras].sort())) {
		change.remote_extras = extras;
	}
	// A library of files says who may ask for what, and where it is sent.
	for (const field of ["discover", "requests"] as const) {
		const given = form.get(field);
		if (given !== null) {
			const was = current[field] ?? "";
			if (String(given) !== was) change[field] = String(given);
		}
	}
	return change;
}

// A library's language or country as its form sends it: "server" for the
// server's own, which the API names "".
export const serverLocale = "server";

function localeChoice(value: FormDataEntryValue | null): string {
	const chosen = String(value ?? serverLocale);
	return chosen === serverLocale ? "" : chosen;
}
