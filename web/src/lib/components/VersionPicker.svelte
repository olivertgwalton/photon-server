<script lang="ts">
import DownloadDialog from "#lib/components/DownloadDialog.svelte";
import { Button } from "#lib/components/ui/button/index.js";
import * as Dialog from "#lib/components/ui/dialog/index.js";
import { getShell } from "#lib/shell.svelte.js";

const shell = getShell();
const asked = shell.versions;
</script>

<Dialog.Root bind:open={asked.open}>
	<Dialog.Content>
		<Dialog.Header>
			<Dialog.Title>
				{asked.use === "play" ? "Play version" : "Download version"}
			</Dialog.Title>
			<Dialog.Description>{asked.title}</Dialog.Description>
		</Dialog.Header>
		{#if !shell.onDisk.length}
			<p class="text-ink-3">None of its versions is on disk.</p>
		{:else}
			<ul class="-mx-2 grid">
				{#each shell.onDisk as v (v.id)}
					<li>
						<Button
							variant="ghost"
							class="w-full justify-start"
							onclick={() => shell.pickVersion(v)}
						>
							{v.display_title}
						</Button>
					</li>
				{/each}
			</ul>
		{/if}
	</Dialog.Content>
</Dialog.Root>

<DownloadDialog
	bind:open={asked.downloading}
	id={asked.id}
	title={asked.title}
	version={shell.toDownload}
/>
