import type { components } from "#lib/api/schema.js";

type Schemas = components["schemas"];

function same(a: readonly string[], b: readonly string[]) {
	return a.length === b.length && a.every((x, i) => x === b[i]);
}

// A library form's fields as a change to the library. The sources and the
// extras are sent only where they differ from what it has, since sending them
// identifies every title in it again.
export function libraryChange(
	form: FormData,
	current?: Schemas["AdminLibrary"],
): Schemas["LibraryChange"] {
	const sources = form.getAll("sources").map(String);
	const extras = form
		.getAll("remote_extras")
		.map(String) as Schemas["ExtraKind"][];
	const change: Schemas["LibraryChange"] = {
		name: String(form.get("name") ?? ""),
		monitor: String(form.get("monitor")) as Schemas["Monitor"],
		previews: String(form.get("previews")) as Schemas["PreviewLevel"],
		markers: String(form.get("markers")) as Schemas["MarkerDetection"],
		keyframes: String(form.get("keyframes")) as Schemas["KeyframeMode"],
		refresh_days: Number(form.get("refresh_days")),
	};
	if (!current || !same(sources, current.sources as string[]))
		change.sources = sources;
	if (
		!current ||
		!same([...extras].sort(), [...current.remote_extras].sort())
	) {
		change.remote_extras = extras;
	}
	return change;
}
