<script lang="ts">
import ListPlusIcon from "@lucide/svelte/icons/list-plus";
import { addToPlaylist, newPlaylist } from "#lib/actions.ts";
import { client } from "#lib/api/client.ts";
import { problemMessage } from "#lib/api/problem.ts";
import { Button } from "#lib/components/ui/button/index.ts";
import * as Dialog from "#lib/components/ui/dialog/index.ts";
import * as Field from "#lib/components/ui/field/index.ts";
import { Input } from "#lib/components/ui/input/index.ts";
import { fields, text } from "#lib/form.ts";
import { getShell } from "#lib/shell.svelte.ts";

const shell = getShell();

const uid = $props.id();

async function add(id: string, listName: string) {
	if (await addToPlaylist(id, shell.picker.ids, listName))
		shell.picker.open = false;
}

async function create(event: SubmitEvent) {
	const name = text(fields(event), "name");
	if (await newPlaylist(name, shell.picker.ids)) shell.picker.open = false;
}
</script>

<Dialog.Root bind:open={shell.picker.open}>
	<Dialog.Content>
		<Dialog.Header>
			<Dialog.Title>Add to playlist</Dialog.Title>
			<Dialog.Description>{shell.picker.title}</Dialog.Description>
		</Dialog.Header>
		<!-- Asked each time it opens, so a list made elsewhere is there. -->
		<svelte:boundary>
			{@const { data, error } = await client().GET("/api/v1/playlists")}
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
			{#snippet pending()}
				<p class="text-ink-3">Loading playlists…</p>
			{/snippet}
		</svelte:boundary>
		<form onsubmit={create}>
			<Field.Field orientation="horizontal">
				<Field.Label for="{uid}-new-playlist" class="sr-only">
					New playlist
				</Field.Label>
				<Input
					id="{uid}-new-playlist"
					name="name"
					placeholder="New playlist"
					required
				/>
				<Button type="submit">Create</Button>
			</Field.Field>
		</form>
	</Dialog.Content>
</Dialog.Root>
