<script lang="ts">
import { act } from "#lib/act.js";
import { serverLocale } from "#lib/admin/library.js";
import { client } from "#lib/api/client.js";
import type { components } from "#lib/api/schema.js";
import Choice from "#lib/components/admin/Choice.svelte";
import { Button } from "#lib/components/ui/button/index.js";
import * as Field from "#lib/components/ui/field/index.js";
import { fields } from "#lib/form.js";

// A film's or show's own metadata language and certification country, over
// its library's, as Jellyfin's item settings.
let { title }: { title: components["schemas"]["TitlePage"] } = $props();

const api = client();
let locales = $state<components["schemas"]["Locales"]>();
$effect(() => {
	api.GET("/api/v1/admin/locales").then(({ data }) => (locales = data));
});

const named = (codes: string[], type: "language" | "region") => {
	const names = new Intl.DisplayNames(undefined, { type });
	return [
		{ value: serverLocale, label: "Its library's" },
		...codes
			.map((value) => ({ value, label: names.of(value) ?? value }))
			.toSorted((a, b) => a.label.localeCompare(b.label)),
	];
};

function save(event: SubmitEvent) {
	const form = fields(event);
	const chosen = (name: string) => {
		const value = String(form.get(name) ?? serverLocale);
		return value === serverLocale ? "" : value;
	};
	return act(
		api.PUT("/api/v1/admin/titles/{id}/locale", {
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

{#if locales}
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
{/if}
