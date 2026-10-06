<script lang="ts">
import FolderIcon from "@lucide/svelte/icons/folder";
import { client } from "#lib/api/client.js";
import { problemMessage } from "#lib/api/problem.js";
import type { components } from "#lib/api/schema.js";
import { Button } from "#lib/components/ui/button/index.js";
import * as Dialog from "#lib/components/ui/dialog/index.js";
import * as Field from "#lib/components/ui/field/index.js";
import { Input } from "#lib/components/ui/input/index.js";
import { Switch } from "#lib/components/ui/switch/index.js";
import { Label } from "#lib/components/ui/label/index.js";

const api = client();

// A folder on the server, typed or found by walking the server's own folders,
// since a path inside a container is rarely the one its owner knows.
let {
	id,
	name,
	value = $bindable(""),
}: { id: string; name: string; value?: string } = $props();

let open = $state(false);
let hidden = $state(false);
let listing = $state<components["schemas"]["FolderList"]>();
let refusal = $state("");

async function browse(path?: string) {
	const { data, error } = await api.GET("/api/v1/admin/folders", {
		params: { query: { path, hidden: hidden ? "show" : undefined } },
	});
	refusal = error ? problemMessage(error) : "";
	if (data) listing = data;
}

function choose() {
	if (listing?.path) value = listing.path;
	open = false;
}
</script>

<div class="flex gap-2">
	<Input
		{id}
		{name}
		bind:value
		required
		placeholder="/media/films"
		spellcheck="false"
		class="font-mono"
	/>
	<Dialog.Root
		bind:open
		onOpenChange={(now) => {
			if (now) browse(value || undefined);
		}}
	>
		<Dialog.Trigger>
			{#snippet child({
				props,
			})}
				<Button {...props} variant="outline">Browse…</Button>
			{/snippet}
		</Dialog.Trigger>
		<Dialog.Content class="sm:max-w-lg">
			<Dialog.Header>
				<Dialog.Title>Choose a folder</Dialog.Title>
				<Dialog.Description class="font-mono break-all">
					{listing?.path ?? "The server's folders"}
				</Dialog.Description>
			</Dialog.Header>
			<div class="flex items-center gap-2">
				<Switch
					id="{id}-hidden"
					bind:checked={hidden}
					onCheckedChange={() => browse(listing?.path)}
				/>
				<Label for="{id}-hidden">Show hidden folders</Label>
			</div>
			<ul class="bg-ground max-h-80 overflow-y-auto rounded-lg p-1">
				{#if listing?.parent}
					<li>
						<button
							type="button"
							class="hover:bg-raise w-full rounded-md px-3 py-2 text-left font-mono text-sm"
							onclick={() => browse(listing?.parent)}
						>
							..
						</button>
					</li>
				{/if}
				{#each listing?.items ?? [] as folder (folder.path)}
					<li>
						<button
							type="button"
							class="hover:bg-raise text-ink flex w-full items-center gap-2 rounded-md px-3 py-2 text-left text-sm"
							onclick={() => browse(folder.path)}
						>
							<FolderIcon
								class="text-ink-3 size-4 shrink-0"
								aria-hidden="true"
							/>
							{folder.name}
						</button>
					</li>
				{:else}
					<li class="text-ink-3 px-3 py-2 text-sm">No folders in here.</li>
				{/each}
			</ul>
			{#if listing?.truncated}
				<p class="text-ink-3 text-sm">
					Only the first folders are listed; type the rest of the path.
				</p>
			{/if}
			<Field.Error errors={[{ message: refusal }]} />
			<Dialog.Footer>
				<Button onclick={choose} disabled={!listing?.path}>
					Choose this folder
				</Button>
			</Dialog.Footer>
		</Dialog.Content>
	</Dialog.Root>
</div>
