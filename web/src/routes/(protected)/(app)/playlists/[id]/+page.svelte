<script lang="ts">
import ArrowDownIcon from "@lucide/svelte/icons/arrow-down";
import ArrowUpIcon from "@lucide/svelte/icons/arrow-up";
import PencilIcon from "@lucide/svelte/icons/pencil";
import PlayIcon from "@lucide/svelte/icons/play";
import ShuffleIcon from "@lucide/svelte/icons/shuffle";
import TrashIcon from "@lucide/svelte/icons/trash";
import XIcon from "@lucide/svelte/icons/x";
import { act } from "#lib/act.js";
import { confirmFirst } from "#lib/actions.svelte.js";
import { client } from "#lib/api/client.js";
import TitleRow from "#lib/components/TitleRow.svelte";
import { Button } from "#lib/components/ui/button/index.js";
import * as Field from "#lib/components/ui/field/index.js";
import { Input } from "#lib/components/ui/input/index.js";
import { count, playHref, runtime } from "#lib/format.js";

let { data } = $props();

const api = client();
const path = $derived({ id: data.playlist.id });
let renaming = $state(false);
let newName = $state("");

async function rename(event: SubmitEvent) {
	event.preventDefault();
	const asked = api.PATCH("/api/v1/playlists/{id}", {
		params: { path },
		body: { name: newName.trim() },
	});
	if (await act(asked, "Renamed.")) renaming = false;
}

function remove() {
	const title = data.playlist.name;
	confirmFirst(
		`Delete ${title}?`,
		"The playlist goes; its titles stay in the library.",
		"Delete",
		() =>
			act(
				api.DELETE("/api/v1/playlists/{id}", { params: { path } }),
				`${title} was deleted.`,
				"/playlists",
			),
	);
}

function move(entry: string, position: number) {
	act(
		api.PUT("/api/v1/playlists/{id}/entries/{entry}/position", {
			params: { path: { ...path, entry } },
			body: { position },
		}),
	);
}

function drop(entry: string) {
	act(
		api.DELETE("/api/v1/playlists/{id}/entries/{entry}", {
			params: { path: { ...path, entry } },
		}),
		"Removed from the playlist.",
	);
}

const first = $derived(data.entries[0]);
const name = (e: (typeof data.entries)[number]) =>
	e.show ? `${e.show.title}: ${e.title}` : e.title;
</script>

<svelte:head><title>{data.playlist.name} · Photon</title></svelte:head>

<div class="grid max-w-4xl gap-6">
	{#if renaming}
		<form onsubmit={rename}>
			<Field.Field orientation="horizontal">
				<Field.Label for="rename" class="sr-only">Name</Field.Label>
				<Input id="rename" bind:value={newName} required class="max-w-md" />
				<Button type="submit">Save</Button>
				<Button variant="ghost" onclick={() => (renaming = false)}>
					Cancel
				</Button>
			</Field.Field>
		</form>
	{:else}
		<h1 class="title">{data.playlist.name}</h1>
	{/if}
	<p class="text-ink-3 text-sm">
		{count(data.entries.length, "title")}{data.playlist.duration_ms
			? ` · ${runtime(data.playlist.duration_ms)}`
			: ""}
	</p>

	<div class="flex flex-wrap gap-2">
		{#if first}
			<Button href={playHref(first.id, { playlist: data.playlist.id })}>
				<PlayIcon class="fill-current" />Play all
			</Button>
			<Button
				href={playHref(first.id, { playlist: data.playlist.id, shuffle: true })}
				variant="outline"
			>
				<ShuffleIcon />Shuffle
			</Button>
		{/if}
		<Button
			variant="outline"
			onclick={() => {
				newName = data.playlist.name;
				renaming = true;
			}}
		>
			<PencilIcon />Rename
		</Button>
		<Button variant="outline" onclick={remove}> <TrashIcon />Delete </Button>
	</div>

	{#if data.entries.length}
		<ol class="divide-line divide-y">
			{#each data.entries as entry, i (entry.entry_id)}
				<li class="py-2">
					<TitleRow card={entry}>
						{#snippet end()}
							<Button
								variant="ghost"
								size="icon-sm"
								disabled={i === 0}
								aria-label="Move {name(entry)} up"
								onclick={() => move(entry.entry_id, i - 1)}
							>
								<ArrowUpIcon />
							</Button>
							<Button
								variant="ghost"
								size="icon-sm"
								disabled={i === data.entries.length - 1}
								aria-label="Move {name(entry)} down"
								onclick={() => move(entry.entry_id, i + 1)}
							>
								<ArrowDownIcon />
							</Button>
							<Button
								variant="ghost"
								size="icon-sm"
								aria-label="Remove {name(entry)}"
								onclick={() => drop(entry.entry_id)}
							>
								<XIcon />
							</Button>
						{/snippet}
					</TitleRow>
				</li>
			{/each}
		</ol>
	{:else}
		<p class="text-ink-3">
			Nothing in this playlist yet. Add titles from their menus.
		</p>
	{/if}
</div>
