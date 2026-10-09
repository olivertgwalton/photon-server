<script lang="ts">
import { act } from "#lib/act.js";
import { removeLibrary, scanLibrary } from "#lib/actions.svelte.js";
import { fields } from "#lib/form.js";
import { libraryChange } from "#lib/admin/library.js";
import { liveStream } from "#lib/admin/stream.svelte.js";
import { client } from "#lib/api/client.js";
import LibraryForm from "#lib/components/admin/LibraryForm.svelte";
import LibraryRefresh from "#lib/components/admin/LibraryRefresh.svelte";
import PageHeader from "#lib/components/PageHeader.svelte";
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

<!-- What can be done to the library is here too, not only on the list. -->
<PageHeader
	title={data.library.name}
	description={data.library.media === "remote"
		? `Streamed, the titles of ${data.library.list?.id}`
		: data.library.root}
>
	{#snippet actions()}
		<Button
			variant="outline"
			size="sm"
			disabled={!!scan}
			onclick={() => scanLibrary(data.library.id, data.library.name)}
		>
			{scan ? "Scanning…" : "Scan now"}
		</Button>
		<LibraryRefresh id={data.library.id} name={data.library.name} />
		<Button
			variant="outline"
			size="sm"
			onclick={() => removeLibrary(data.library.id, data.library.name)}
		>
			Remove <span class="sr-only">{data.library.name}</span>
		</Button>
	{/snippet}
</PageHeader>

<form onsubmit={save} class="grid max-w-2xl gap-6">
	{#if scan}
		<ScanProgress {scan} name={data.library.name} />
	{/if}
	{#key data.library}
		<LibraryForm
			library={data.library}
			providers={data.providers}
			locales={data.locales}
			serverLanguage={data.serverLanguage}
			serverCountry={data.serverCountry}
		/>
	{/key}
	<div class="flex gap-2">
		<Button type="submit">Save</Button>
		<Button href="/settings/server/libraries" variant="outline"
			>Back to libraries</Button
		>
	</div>
</form>
