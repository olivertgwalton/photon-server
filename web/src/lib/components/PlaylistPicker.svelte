<script lang="ts">
import ListPlusIcon from "@lucide/svelte/icons/list-plus";
import { addToPlaylist, newPlaylist, picker } from "#lib/actions.svelte.js";
import { client } from "#lib/api/client.js";
import { problemMessage } from "#lib/api/problem.js";
import { Button } from "#lib/components/ui/button/index.js";
import * as Dialog from "#lib/components/ui/dialog/index.js";
import * as Field from "#lib/components/ui/field/index.js";
import { Input } from "#lib/components/ui/input/index.js";
import { fields } from "#lib/form.js";

async function add(id: string, listName: string) {
	if (await addToPlaylist(id, picker.ids, listName)) picker.open = false;
}

async function create(event: SubmitEvent) {
	const name = String(fields(event).get("name")).trim();
	if (await newPlaylist(name, picker.ids)) picker.open = false;
}
</script>

<Dialog.Root bind:open={picker.open}>
	<Dialog.Content>
		<Dialog.Header>
			<Dialog.Title>Add to playlist</Dialog.Title>
			<Dialog.Description>{picker.title}</Dialog.Description>
		</Dialog.Header>
		<!-- Asked each time it opens, so a list made elsewhere is there. -->
		{#await client().GET("/api/v1/playlists")}
			<p class="text-ink-3">Loading playlists…</p>
		{:then { data, error }}
			{#if !data}
				<p role="alert" class="text-destructive">{problemMessage(error)}</p>
			{:else if data.items.length}
				<ul class="-mx-2 grid max-h-64 overflow-y-auto">
					{#each data.items as playlist (playlist.id)}
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
		{/await}
		<form onsubmit={create}>
			<Field.Field orientation="horizontal">
				<Field.Label for="new-playlist" class="sr-only">
					New playlist
				</Field.Label>
				<Input
					id="new-playlist"
					name="name"
					placeholder="New playlist"
					required
				/>
				<Button type="submit">Create</Button>
			</Field.Field>
		</form>
	</Dialog.Content>
</Dialog.Root>
