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

type Version = components["schemas"]["VersionPage"];

// Which copy to play or keep, asked of a title's copies on disk.
let downloading = $state(false);
let copy = $state<Version>();

// Its copies on disk, asked each time it opens. One alone is no choice: it
// plays, or its download is asked about, at once.
async function onDisk(): Promise<{ versions?: Version[]; failed?: string }> {
	const { data, error } = await client().GET("/api/v1/titles/{id}", {
		params: { path: { id: asked.id } },
	});
	if (!data) return { failed: problemMessage(error) };
	const versions = (data.versions ?? []).filter((v) => !v.missing_since);
	if (versions.length === 1) choose(versions[0]);
	return { versions };
}

function choose(v: Version) {
	asked.open = false;
	if (asked.use === "play") {
		goto(playHref(asked.id, { version: v.id }));
		return;
	}
	copy = v;
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
		{#await onDisk()}
			<p class="text-ink-3">Loading versions…</p>
		{:then { versions, failed }}
			{#if failed}
				<p role="alert" class="text-destructive">{failed}</p>
			{:else if !versions?.length}
				<p class="text-ink-3">None of its copies is on disk.</p>
			{:else}
				<ul class="-mx-2 grid">
					{#each versions as v (v.id)}
						<li>
							<Button
								variant="ghost"
								class="w-full justify-start"
								onclick={() => choose(v)}
							>
								{versionName(v)}
							</Button>
						</li>
					{/each}
				</ul>
			{/if}
		{/await}
	</Dialog.Content>
</Dialog.Root>

<DownloadDialog
	bind:open={downloading}
	id={asked.id}
	title={asked.title}
	version={copy}
/>
