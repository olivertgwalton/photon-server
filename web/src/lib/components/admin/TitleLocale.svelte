<script lang="ts">
import { act } from "#lib/act.js";
import { localeOptions, serverLocale } from "#lib/admin/library.js";
import { client } from "#lib/api/client.js";
import type { components } from "#lib/api/schema.js";
import Choice from "#lib/components/Choice.svelte";
import { Button } from "#lib/components/ui/button/index.js";
import * as Field from "#lib/components/ui/field/index.js";
import { fields } from "#lib/form.js";

type Schemas = components["schemas"];

// A film's or show's own metadata language and certification country, over
// its library's, as Jellyfin's item settings.
let {
	title,
	locales,
}: { title: Schemas["TitlePage"]; locales: Schemas["Locales"] } = $props();

const named = (codes: string[], type: "language" | "region") => [
	{ value: serverLocale, label: "Its library's" },
	...localeOptions(codes, type),
];

function save(event: SubmitEvent) {
	const form = fields(event);
	const chosen = (name: string) => {
		const value = String(form.get(name) ?? serverLocale);
		return value === serverLocale ? "" : value;
	};
	return act(
		client().PUT("/api/v1/admin/titles/{id}/locale", {
			params: { path: { id: title.id } },
			body: {
				metadata_language: chosen("metadata_language"),
				certification_country: chosen("certification_country"),
			},
		}),
		"Saved. It is described again in them in a moment.",
	);
}
</script>

<form onsubmit={save} class="grid gap-4">
	<div class="grid gap-4 sm:grid-cols-2">
		<Field.Field>
			<Field.Label for="title-language">Metadata language</Field.Label>
			<Choice
				id="title-language"
				name="metadata_language"
				value={title.metadata_language ?? serverLocale}
				options={named(locales.languages, "language")}
			/>
		</Field.Field>
		<Field.Field>
			<Field.Label for="title-country">Certification country</Field.Label>
			<Choice
				id="title-country"
				name="certification_country"
				value={title.certification_country ?? serverLocale}
				options={named(locales.countries, "region")}
			/>
		</Field.Field>
	</div>
	<div><Button type="submit" variant="outline">Save language</Button></div>
</form>
