<script lang="ts">
import { act, fields } from "#lib/admin/act.js";
import { libraryChange } from "#lib/admin/library.js";
import { liveStream } from "#lib/admin/stream.svelte.js";
import { client } from "#lib/api/client.js";
import LibraryForm from "#lib/components/admin/LibraryForm.svelte";
import ScanProgress from "#lib/components/admin/ScanProgress.svelte";
import { Button } from "#lib/components/ui/button/index.js";

let { data } = $props();

const live = liveStream();
const scan = $derived(
	live.state.scans.find((s) => s.library_id === data.library.id),
);

function save(event: SubmitEvent) {
	const form = fields(event);
	return act(
		client().PATCH("/api/v1/admin/libraries/{id}", {
			params: { path: { id: data.library.id } },
			body: libraryChange(form, data.library),
		}),
		"Saved.",
	);
}
</script>

<svelte:head
	><title>{data.library.name} · Dashboard · Photon</title></svelte:head
>

<form onsubmit={save} class="grid max-w-2xl gap-6">
	<h1 class="title">{data.library.name}</h1>
	{#if scan}
		<ScanProgress {scan} name={data.library.name} />
	{/if}
	{#key data.library}
		<LibraryForm library={data.library} providers={data.providers} />
	{/key}
	<div class="flex gap-2">
		<Button type="submit">Save</Button>
		<Button href="/admin/libraries" variant="outline">Back to libraries</Button>
	</div>
</form>
