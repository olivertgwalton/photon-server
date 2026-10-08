<script lang="ts">
import { act } from "#lib/act.js";
import { localeOptions } from "#lib/admin/library.js";
import { client } from "#lib/api/client.js";
import type { components } from "#lib/api/schema.js";
import Choice from "#lib/components/Choice.svelte";
import { Button } from "#lib/components/ui/button/index.js";
import * as Field from "#lib/components/ui/field/index.js";
import { Input } from "#lib/components/ui/input/index.js";
import { fields } from "#lib/form.js";

type Schemas = components["schemas"];

// What the server is called and what its metadata is asked in, as Jellyfin's
// general settings. Saved, it goes `to` a page where one is given.
let {
	settings,
	locales,
	submit = "Save",
	to,
}: {
	settings: Schemas["ServerSettings"];
	locales: Schemas["Locales"];
	submit?: string;
	to?: string;
} = $props();

const languages = $derived(localeOptions(locales.languages, "language"));
const countries = $derived(localeOptions(locales.countries, "region"));

function save(event: SubmitEvent) {
	const form = fields(event);
	return act(
		client().PUT("/api/v1/admin/server", {
			body: {
				name: String(form.get("name") ?? ""),
				metadata_language: String(form.get("metadata_language") ?? ""),
				certification_country: String(form.get("certification_country") ?? ""),
			},
		}),
		to ? undefined : "Saved. Every server goes by it now.",
		to,
	);
}
</script>

<form onsubmit={save} class="grid max-w-2xl gap-6">
	<Field.Field>
		<Field.Label for="server-name">Name</Field.Label>
		<Input
			id="server-name"
			name="name"
			value={settings.name}
			autocomplete="off"
			required
		/>
		<Field.Description>
			What apps and the sign-in page call this server.
		</Field.Description>
	</Field.Field>
	<div class="grid gap-6 sm:grid-cols-2">
		<Field.Field>
			<Field.Label for="server-language">Metadata language</Field.Label>
			<Choice
				id="server-language"
				name="metadata_language"
				value={settings.metadata_language}
				options={languages}
			/>
			<Field.Description>
				What titles are described in, where a library asks in none of its own.
			</Field.Description>
		</Field.Field>
		<Field.Field>
			<Field.Label for="server-country">Certification country</Field.Label>
			<Choice
				id="server-country"
				name="certification_country"
				value={settings.certification_country}
				options={countries}
			/>
			<Field.Description>
				Whose age ratings titles are matched with. A rating already found keeps
				the country it came from.
			</Field.Description>
		</Field.Field>
	</div>
	<div><Button type="submit">{submit}</Button></div>
</form>
