<script lang="ts">
import PageHeader from "#lib/components/PageHeader.svelte";
import { goto } from "$app/navigation";
import { fields } from "#lib/form.js";
import { addLibrary } from "#lib/admin/add.js";
import LibraryForm from "#lib/components/admin/LibraryForm.svelte";
import { Button } from "#lib/components/ui/button/index.js";

let { data } = $props();

let adding = $state(false);

async function add(event: SubmitEvent) {
	adding = true;
	const added = await addLibrary(fields(event));
	adding = false;
	if (added) await goto("/settings/server/libraries", { refreshAll: true });
}
</script>

<PageHeader
	title="Add a library"
	description="A folder of films or of shows. It is scanned as soon as it is added, and watched for changes after."
/>

<form onsubmit={add} class="grid max-w-2xl gap-6">
	<LibraryForm
		providers={data.providers}
		locales={data.locales}
		serverLanguage={data.serverLanguage}
		serverCountry={data.serverCountry}
	/>
	<div class="flex gap-2">
		<Button type="submit" disabled={adding}>Add and scan</Button>
		<Button href="/settings/server/libraries" variant="outline">Cancel</Button>
	</div>
</form>
