<script lang="ts">
import { act } from "#lib/act.js";
import { scanLibrary } from "#lib/actions.js";
import { libraryChange } from "#lib/admin/library.js";
import { liveStream } from "#lib/admin/stream.svelte.js";
import { client } from "#lib/api/client.js";
import LibraryForm from "#lib/components/admin/LibraryForm.svelte";
import ScanProgress from "#lib/components/admin/ScanProgress.svelte";
import PageHeader from "#lib/components/PageHeader.svelte";
import { Button } from "#lib/components/ui/button/index.js";
import { fields } from "#lib/form.js";
import { getShell } from "#lib/shell.svelte.js";
import { page } from "$app/state";

let { data } = $props();
const shell = getShell();

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
		? `Streamed, the titles of ${data.library.list?.id ?? "what a search finds"}`
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
		<Button
			variant="outline"
			size="sm"
			onclick={() =>
				shell.refreshMetadata("library", data.library.id, data.library.name)}
		>
			Refresh metadata <span class="sr-only">of {data.library.name}</span>
		</Button>
		{#if data.library.media !== "remote"}
			<Button
				variant="outline"
				size="sm"
				onclick={() =>
					shell.askToForgetMissingVersions(data.library.id, data.library.name)}
			>
				Forget versions on no disk
				<span class="sr-only">in {data.library.name}</span>
			</Button>
		{/if}
		<Button
			variant="outline"
			size="sm"
			onclick={() =>
				shell.askToRemoveLibrary(
					data.library.id,
					data.library.name,
					page.url.pathname,
				)}
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
			kinds={data.kinds}
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
