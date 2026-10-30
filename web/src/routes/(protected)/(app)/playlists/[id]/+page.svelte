<script lang="ts">
import ArrowDownIcon from "@lucide/svelte/icons/arrow-down";
import ArrowUpIcon from "@lucide/svelte/icons/arrow-up";
import FilterIcon from "@lucide/svelte/icons/filter";
import PencilIcon from "@lucide/svelte/icons/pencil";
import PlayIcon from "@lucide/svelte/icons/play";
import ShuffleIcon from "@lucide/svelte/icons/shuffle";
import TrashIcon from "@lucide/svelte/icons/trash";
import XIcon from "@lucide/svelte/icons/x";
import { act } from "#lib/act.ts";
import { withQuery } from "#lib/address.ts";
import { client } from "#lib/api/client.ts";
import TitleRow from "#lib/components/TitleRow.svelte";
import { Button } from "#lib/components/ui/button/index.ts";
import * as Field from "#lib/components/ui/field/index.ts";
import { Input } from "#lib/components/ui/input/index.ts";
import { count, runtime, titleWithShow } from "#lib/format.ts";
import { playQueue } from "#lib/queue.ts";
import { getShell } from "#lib/shell.svelte.ts";
import { ruleQuery, wallSearch } from "#lib/wall.ts";
import { page } from "$app/state";

let { data } = $props();
const shell = getShell();

const api = client();
const path = $derived({ id: data.playlist.id });
// A smart playlist plays what its rule finds, so its entries are not its own
// to order or take out: the rule is changed on the wall it reads.
const rule = $derived(data.playlist.rule);
const filters = $derived(
	rule
		? withQuery(
				new URL(
					`/libraries/${data.playlist.library_id}${wallSearch(ruleQuery(rule))}`,
					page.url.href,
				),
				{ playlist: data.playlist.id },
			)
		: undefined,
);
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
	shell.confirmFirst(
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
		{#if rule}
			Smart playlist ·
		{/if}
		{count(data.entries.length, "title")}{data.playlist.duration_ms
			? ` · ${runtime(data.playlist.duration_ms)}`
			: ""}
	</p>

	<div class="flex flex-wrap gap-2">
		{#if data.entries.length}
			<Button onclick={() => playQueue(data.entries)}>
				<PlayIcon class="fill-current" />Play all
			</Button>
			<Button onclick={() => playQueue(data.entries, true)} variant="outline">
				<ShuffleIcon />Shuffle
			</Button>
		{/if}
		{#if filters}
			<Button variant="outline" href={filters}>
				<FilterIcon />Change the filters
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
			{#each data.entries as entry, i (entry.entry_id ?? entry.id)}
				<li class="py-2">
					<TitleRow card={entry}>
						{#snippet end()}
							<!-- A rule's finding is no entry of its own to move or take out. -->
							{#if entry.entry_id}
								{@const entryID = entry.entry_id}
								<Button
									variant="ghost"
									size="icon-sm"
									disabled={i === 0}
									aria-label="Move {titleWithShow(entry)} up"
									onclick={() => move(entryID, i - 1)}
								>
									<ArrowUpIcon />
								</Button>
								<Button
									variant="ghost"
									size="icon-sm"
									disabled={i === data.entries.length - 1}
									aria-label="Move {titleWithShow(entry)} down"
									onclick={() => move(entryID, i + 1)}
								>
									<ArrowDownIcon />
								</Button>
								<Button
									variant="ghost"
									size="icon-sm"
									aria-label="Remove {titleWithShow(entry)}"
									onclick={() => drop(entryID)}
								>
									<XIcon />
								</Button>
							{/if}
						{/snippet}
					</TitleRow>
				</li>
			{/each}
		</ol>
	{:else if rule}
		<p class="text-ink-3">Nothing in the library matches these filters.</p>
	{:else}
		<p class="text-ink-3">
			Nothing in this playlist yet. Add titles from their menus.
		</p>
	{/if}
</div>
