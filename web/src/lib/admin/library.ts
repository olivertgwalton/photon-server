import type { components } from "#lib/api/schema.js";
import { textAsTyped } from "#lib/form.js";

type Schemas = components["schemas"];
type Ranked = Schemas["RankedSource"];

type Fetcher = "metadata" | "images";

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
				name: String(id).charAt(0).toUpperCase() + String(id).slice(1),
				ready: true,
			})),
		...providers
			.filter((p) =>
				(f === "metadata" ? p.metadata_kinds : p.image_kinds).includes(kind),
			)
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

// A library form's fields as a change to the library. The sources and the
// extras are sent only where they differ from what it has, since sending them
// identifies every title in it again.
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
		// copies of, and the form asks none of those of one.
		monitor: (form.get("monitor") ?? undefined) as
			| Schemas["Monitor"]
			| undefined,
		previews: String(form.get("previews")) as Schemas["PreviewLevel"],
		markers: String(form.get("markers")) as Schemas["MarkerDetection"],
		keyframes: String(form.get("keyframes")) as Schemas["KeyframeMode"],
		themes: String(form.get("themes")) as Schemas["ThemeLookup"],
		deletion: (form.get("deletion") ?? undefined) as
			| Schemas["MediaDeletion"]
			| undefined,
		missing_copies: (form.get("missing_copies") ?? undefined) as
			| Schemas["MissingCopy"]
			| undefined,
		metadata_language: localeChoice(form.get("metadata_language")),
		certification_country: localeChoice(form.get("certification_country")),
		artwork_language: String(
			form.get("artwork_language"),
		) as Schemas["ArtworkLanguage"],
		title_language: String(
			form.get("title_language"),
		) as Schemas["TitleLanguage"],
		box_sets: String(form.get("box_sets")) as Schemas["BoxSetMode"],
		refresh_days: Number(form.get("refresh_days")),
		subtitle_languages: form.getAll("subtitle_languages").map(String),
		subtitle_match: String(
			form.get("subtitle_match"),
		) as Schemas["SubtitleMatch"],
	};
	const sources: Schemas["KindSourcesChange"][] = [];
	for (const has of current.sources) {
		const asked: Schemas["KindSourcesChange"] = { kind: has.kind };
		for (const f of ["metadata", "images"] as const) {
			const given = form.get(`sources-${has.kind}-${f}`);
			if (given === null) continue;
			const list = JSON.parse(String(given)) as Ranked[];
			if (said(list) !== said(has[f])) asked[f] = list;
		}
		if (asked.metadata || asked.images) sources.push(asked);
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
