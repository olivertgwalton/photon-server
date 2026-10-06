<script lang="ts">
import ListPlusIcon from "@lucide/svelte/icons/list-plus";
import { addToPlaylist, newPlaylist, picker } from "#lib/actions.svelte.js";
import { client } from "#lib/api/client.js";
import { problemMessage } from "#lib/api/problem.js";
import type { components } from "#lib/api/schema.js";
import { Button } from "#lib/components/ui/button/index.js";
import * as Dialog from "#lib/components/ui/dialog/index.js";
import * as Field from "#lib/components/ui/field/index.js";
import { Input } from "#lib/components/ui/input/index.js";

let playlists = $state<components["schemas"]["Playlist"][]>();
let failed = $state("");
let name = $state("");

// Asked each time it opens, so a list made elsewhere is there.
$effect(() => {
	if (!picker.open) return;
	playlists = undefined;
	failed = "";
	client()
		.GET("/api/v1/playlists")
		.then(({ data, error }) => {
			if (data) playlists = data.items;
			else failed = problemMessage(error);
		});
});

async function add(id: string, listName: string) {
	if (await addToPlaylist(id, picker.ids, listName)) picker.open = false;
}

async function create(event: SubmitEvent) {
	event.preventDefault();
	if (await newPlaylist(name.trim(), picker.ids)) {
		picker.open = false;
		name = "";
	}
}
</script>

<Dialog.Root bind:open={picker.open}>
	<Dialog.Content>
		<Dialog.Header>
			<Dialog.Title>Add to playlist</Dialog.Title>
			<Dialog.Description>{picker.title}</Dialog.Description>
		</Dialog.Header>
		{#if failed}
			<p role="alert" class="text-destructive">{failed}</p>
		{:else if !playlists}
			<p class="text-ink-3">Loading playlists…</p>
		{:else if playlists.length}
			<ul class="-mx-2 grid max-h-64 overflow-y-auto">
				{#each playlists as playlist (playlist.id)}
					<li>
						<Button
							variant="ghost"
							class="w-full justify-start"
							onclick={() => add(playlist.id, playlist.name)}
						>
							<ListPlusIcon />
							{playlist.name}
							<span class="text-ink-3 ml-auto">{playlist.entries}</span>
						</Button>
					</li>
				{/each}
			</ul>
		{/if}
		<form onsubmit={create}>
			<Field.Field orientation="horizontal">
				<Field.Label for="new-playlist" class="sr-only">
					New playlist
				</Field.Label>
				<Input
					id="new-playlist"
					bind:value={name}
					placeholder="New playlist"
					required
				/>
				<Button type="submit" disabled={!name.trim()}>Create</Button>
			</Field.Field>
		</form>
	</Dialog.Content>
</Dialog.Root>
