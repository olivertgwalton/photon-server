<script lang="ts">
import { goto } from "$app/navigation";
import { versions as asked } from "#lib/actions.svelte.js";
import { client } from "#lib/api/client.js";
import { problemMessage } from "#lib/api/problem.js";
import type { components } from "#lib/api/schema.js";
import DownloadDialog from "#lib/components/DownloadDialog.svelte";
import { Button } from "#lib/components/ui/button/index.js";
import * as Dialog from "#lib/components/ui/dialog/index.js";
import { playHref, versionName } from "#lib/format.js";

// Which copy to play or keep, asked of a title's copies on disk. One alone is
// no choice: it plays, or its download is asked about, at once.
let onDisk = $state<components["schemas"]["VersionPage"][]>();
let failed = $state("");
let downloading = $state(false);
let version = $state<string>();

$effect(() => {
	if (!asked.open) return;
	onDisk = undefined;
	failed = "";
	client()
		.GET("/api/v1/titles/{id}", { params: { path: { id: asked.id } } })
		.then(({ data, error }) => {
			if (!data) {
				failed = problemMessage(error);
				return;
			}
			onDisk = (data.versions ?? []).filter((v) => !v.missing_since);
			if (onDisk.length === 1) choose(onDisk[0].id);
		});
});

function choose(id: string) {
	asked.open = false;
	if (asked.use === "play") {
		goto(playHref(asked.id, { version: id }));
		return;
	}
	version = id;
	downloading = true;
}
</script>

<Dialog.Root bind:open={asked.open}>
	<Dialog.Content>
		<Dialog.Header>
			<Dialog.Title>
				{asked.use === "play" ? "Play version" : "Download version"}
			</Dialog.Title>
			<Dialog.Description>{asked.title}</Dialog.Description>
		</Dialog.Header>
		{#if failed}
			<p role="alert" class="text-destructive">{failed}</p>
		{:else if !onDisk}
			<p class="text-ink-3">Loading versions…</p>
		{:else if !onDisk.length}
			<p class="text-ink-3">None of its copies is on disk.</p>
		{:else}
			<ul class="-mx-2 grid">
				{#each onDisk as v (v.id)}
					<li>
						<Button
							variant="ghost"
							class="w-full justify-start"
							onclick={() => choose(v.id)}
						>
							{versionName(v)}
						</Button>
					</li>
				{/each}
			</ul>
		{/if}
	</Dialog.Content>
</Dialog.Root>

<DownloadDialog
	bind:open={downloading}
	id={asked.id}
	title={asked.title}
	{version}
/>
