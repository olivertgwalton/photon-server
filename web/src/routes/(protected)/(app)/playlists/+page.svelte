<script lang="ts">
import ListVideoIcon from "@lucide/svelte/icons/list-video";
import { toast } from "svelte-sonner";
import { goto } from "$app/navigation";
import { client } from "#lib/api/client.js";
import { problemMessage } from "#lib/api/problem.js";
import { Button } from "#lib/components/ui/button/index.js";
import * as Field from "#lib/components/ui/field/index.js";
import { Input } from "#lib/components/ui/input/index.js";
import { fields } from "#lib/form.js";
import { count, runtime } from "#lib/format.js";

let { data } = $props();

async function create(event: SubmitEvent) {
	const form = fields(event);
	const { data: made, error } = await client().POST("/api/v1/playlists", {
		body: { name: String(form.get("name")).trim() },
	});
	if (error) toast.error(problemMessage(error));
	else goto(`/playlists/${made.id}`);
}
</script>

<svelte:head><title>Playlists · Photon</title></svelte:head>

<div class="grid max-w-4xl gap-8">
	<h1 class="title">Playlists</h1>

	<form onsubmit={create}>
		<Field.Field orientation="horizontal" class="max-w-md">
			<Field.Label for="playlist-name" class="sr-only">
				New playlist
			</Field.Label>
			<Input
				id="playlist-name"
				name="name"
				placeholder="New playlist"
				required
			/>
			<Button type="submit">Create</Button>
		</Field.Field>
	</form>

	{#if data.playlists.length}
		<ul class="divide-line divide-y">
			{#each data.playlists as playlist (playlist.id)}
				<li>
					<a
						href="/playlists/{playlist.id}"
						class="hover:bg-accent -mx-3 flex items-center gap-4 rounded-lg px-3 py-3"
					>
						<span
							class="bg-raise text-ink-3 grid size-12 shrink-0 place-items-center rounded-md"
						>
							<ListVideoIcon aria-hidden="true" />
						</span>
						<span class="min-w-0">
							<span class="text-ink block truncate font-semibold">
								{playlist.name}
							</span>
							<span class="text-ink-3 block text-sm">
								{count(playlist.entries, "title")}{playlist.duration_ms
									? ` · ${runtime(playlist.duration_ms)}`
									: ""}
							</span>
						</span>
					</a>
				</li>
			{/each}
		</ul>
	{:else}
		<p class="text-ink-3">
			No playlists yet. Make one here, or add a title to one from its menu.
		</p>
	{/if}
</div>
