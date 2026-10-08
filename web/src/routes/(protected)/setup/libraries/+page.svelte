<script lang="ts">
import { refreshAll } from "$app/navigation";
import { addLibrary } from "#lib/admin/add.js";
import LibraryForm from "#lib/components/admin/LibraryForm.svelte";
import { Button } from "#lib/components/ui/button/index.js";
import * as Card from "#lib/components/ui/card/index.js";
import { fields } from "#lib/form.js";

let { data } = $props();
let adding = $state(false);

// Each library added is listed, and the form is ready for the next, as
// Jellyfin's wizard adds several before going on.
async function add(event: SubmitEvent) {
	const form = event.currentTarget as HTMLFormElement;
	adding = true;
	const added = await addLibrary(fields(event));
	adding = false;
	if (!added) return;
	form.reset();
	await refreshAll();
}
</script>

<svelte:head><title>Add libraries · Set up · Photon</title></svelte:head>

<Card.Root class="w-full max-w-3xl">
	<Card.Header>
		<Card.Title><h1 class="heading text-xl">Add your libraries</h1></Card.Title>
		<Card.Description>
			A folder of films or of shows, at its path on the server. Each is scanned
			as soon as it is added. More can be added later under Settings.
		</Card.Description>
	</Card.Header>
	<Card.Content class="grid gap-6">
		{#if data.libraries.length}
			<ul class="grid gap-1 text-sm" aria-label="Libraries added">
				{#each data.libraries as library (library.id)}
					<li>
						<span class="text-ink font-medium">{library.name}</span>
						<span class="text-ink-3 font-mono">{library.root}</span>
					</li>
				{/each}
			</ul>
		{/if}
		<form onsubmit={add} class="grid gap-6">
			<LibraryForm
				providers={data.providers}
				locales={data.locales}
				serverLanguage={data.server.metadata_language}
				serverCountry={data.server.certification_country}
			/>
			<div class="flex flex-wrap gap-2">
				<Button type="submit" variant="outline" disabled={adding}>
					Add and scan
				</Button>
				<Button href="/setup/remote">
					{data.libraries.length ? "Next" : "Skip for now"}
				</Button>
			</div>
		</form>
	</Card.Content>
</Card.Root>
