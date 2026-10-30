<script lang="ts">
import { act } from "#lib/act.ts";
import { client } from "#lib/api/client.ts";
import type { components } from "#lib/api/schema.d.ts";
import Choice from "#lib/components/Choice.svelte";
import { Button } from "#lib/components/ui/button/index.ts";
import * as Field from "#lib/components/ui/field/index.ts";
import { Input } from "#lib/components/ui/input/index.ts";
import { fields, textAsTyped } from "#lib/form.ts";
import { country, localeOptions } from "#lib/format.ts";

type Schemas = components["schemas"];
type ReleaseCheck = Schemas["ReleaseCheck"];

// What the server is called, what its metadata is asked in, as Jellyfin's
// general settings, and whether it looks for new versions, as Plex's. Saved, it
// goes `to` a page where one is given.
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

const languages = $derived(localeOptions(locales.languages));
const countries = $derived(localeOptions(locales.countries, country));

const releaseChecks: readonly { value: ReleaseCheck; label: string }[] = [
	{ value: "on", label: "On" },
	{ value: "off", label: "Off" },
];
// What is chosen, until a save loads the page again.
let releaseCheck = $derived<ReleaseCheck>(settings.release_check);

function save(event: SubmitEvent) {
	const form = fields(event);
	return act(
		client().PUT("/api/v1/admin/server", {
			body: {
				name: textAsTyped(form, "name"),
				metadata_language: textAsTyped(form, "metadata_language"),
				certification_country: textAsTyped(form, "certification_country"),
				release_check: releaseCheck,
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
	<Field.Field>
		<Field.Label for="release-check">Look for new versions</Field.Label>
		<Choice
			id="release-check"
			bind:value={releaseCheck}
			options={releaseChecks}
			class="w-48"
		/>
		<Field.Description>
			Asks GitHub a few times a day whether a newer Photon is out, and says so
			on the dashboard.
		</Field.Description>
	</Field.Field>
	<div><Button type="submit">{submit}</Button></div>
</form>
