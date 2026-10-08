<script lang="ts">
import ArrowDownIcon from "@lucide/svelte/icons/arrow-down";
import ArrowUpIcon from "@lucide/svelte/icons/arrow-up";
import XIcon from "@lucide/svelte/icons/x";
import { client } from "#lib/api/client.js";
import type { components } from "#lib/api/schema.js";
import ConfirmButton from "#lib/components/admin/ConfirmButton.svelte";
import { Button } from "#lib/components/ui/button/index.js";
import { Input } from "#lib/components/ui/input/index.js";
import { act } from "#lib/act.js";
import { ruleQuery, wallSearch } from "#lib/wall.js";

const api = client();

type Card = components["schemas"]["Card"];

let { data } = $props();

const path = $derived({ params: { path: { id: data.collection.id } } });

function save(event: SubmitEvent) {
	event.preventDefault();
	return act(
		api.PUT("/api/v1/admin/collections/{id}/members", {
			...path,
			body: { item_ids: members.map((m) => m.id) },
		}),
		"Saved.",
	);
}

function remove() {
	return act(
		api.DELETE("/api/v1/admin/collections/{id}", path),
		`${data.collection.title} was removed.`,
		"/settings/server/collections",
	);
}

const editable = $derived(data.collection.origin === "user");
const smart = $derived(data.collection.rule);
const list = $derived(data.collection.list);
const listSources: Record<string, string> = {
	tmdb: "TMDB",
	mdblist: "MDBList",
};

function sync() {
	return act(
		api.POST("/api/v1/admin/collections/{id}/sync", path),
		"Read again.",
	);
}
// Its library's wall, filtered by its rule, where the rule is changed.
const changeRule = $derived.by(() => {
	if (!smart || !data.library) return undefined;
	const search = wallSearch(ruleQuery(smart));
	return `/libraries/${data.library}${search}${search ? "&" : "?"}collection=${data.collection.id}`;
});

let members = $state<Card[]>([]);
$effect.pre(() => {
	members = [...data.members];
});

let query = $state("");
let found = $state<Card[]>([]);

async function search(event: SubmitEvent) {
	event.preventDefault();
	const { data: answer } = await api.GET("/api/v1/search", {
		params: { query: { q: query, library: data.library, limit: 12 } },
	});
	found = (answer?.titles.items ?? []).filter(
		(c) =>
			(c.kind === "movie" || c.kind === "show") &&
			!members.some((m) => m.id === c.id),
	);
}

function move(from: number, to: number) {
	const next = [...members];
	const [moved] = next.splice(from, 1);
	if (moved) next.splice(to, 0, moved);
	members = next;
}

function caption(card: Card) {
	return card.year ? `${card.title} (${card.year})` : card.title;
}
</script>

<svelte:head
	><title>{data.collection.title} · Settings · Photon</title></svelte:head
>

<div class="grid max-w-2xl gap-6">
	<div>
		<h1 class="title">{data.collection.title}</h1>
		<p class="text-ink-3 mt-1 text-sm">
			{#if editable}
				Made here.
				<a href="/settings/server/titles/{data.collection.id}" class="underline"
					>Rename it, or choose its artwork</a
				>.
			{:else if list}
				Holds the titles of
				{listSources[String(list.source)] ?? list.source}
				list
				<span class="font-mono">{list.id}</span>
				the library has, read again daily.
				{#if list.missing}
					{list.missing}
					of it {list.missing === 1 ? "is" : "are"} not in the library.
				{/if}
			{:else if smart}
				A smart collection: it holds what its filters find
				{smart.limit ? `, the first ${smart.limit}` : ""}, found again as the
				library changes, and the same for everyone.
				{#if changeRule}
					<a href={changeRule} class="underline">Change its filters</a>.
				{/if}
			{:else}
				Made by TMDB as it matched these films, so it follows TMDB and is not
				edited here.
			{/if}
		</p>
	</div>

	<form onsubmit={save} class="grid gap-4">
		<ol class="divide-line border-line divide-y rounded-lg border">
			{#each members as member, i (member.id)}
				<li class="flex items-center gap-3 px-3 py-2">
					<a
						href="/titles/{member.id}"
						class="text-ink min-w-0 flex-1 truncate text-sm hover:underline"
					>
						{caption(member)}
					</a>
					{#if editable}
						<Button
							variant="ghost"
							size="icon-sm"
							aria-label="Move {member.title} up"
							disabled={i === 0}
							onclick={() => move(i, i - 1)}
						>
							<ArrowUpIcon />
						</Button>
						<Button
							variant="ghost"
							size="icon-sm"
							aria-label="Move {member.title} down"
							disabled={i === members.length - 1}
							onclick={() => move(i, i + 1)}
						>
							<ArrowDownIcon />
						</Button>
						<Button
							variant="ghost"
							size="icon-sm"
							aria-label="Take {member.title} out"
							onclick={() =>
								(members = members.filter((m) => m.id !== member.id))}
						>
							<XIcon />
						</Button>
					{/if}
				</li>
			{:else}
				<li class="text-ink-3 px-3 py-2 text-sm">Nothing in it yet.</li>
			{/each}
		</ol>
		{#if editable}
			<Button type="submit" class="justify-self-start">Save the order</Button>
		{/if}
	</form>

	{#if editable}
		<section aria-labelledby="add" class="grid gap-3">
			<h2 id="add" class="heading">Add titles</h2>
			<form onsubmit={search} class="flex gap-2">
				<Input
					bind:value={query}
					type="search"
					aria-label="Find a title"
					placeholder="Find a title"
					required
				/>
				<Button type="submit" variant="outline">Find</Button>
			</form>
			{#if found.length}
				<ul class="grid gap-1">
					{#each found as card (card.id)}
						<li class="flex items-center justify-between gap-3 text-sm">
							<span class="text-ink truncate">{caption(card)}</span>
							<Button
								variant="outline"
								size="sm"
								onclick={() => {
									members = [...members, card];
									found = found.filter((c) => c.id !== card.id);
								}}
							>
								Add <span class="sr-only">{card.title}</span>
							</Button>
						</li>
					{/each}
				</ul>
				<p class="text-ink-3 text-xs">
					Added titles are kept once the order is saved.
				</p>
			{/if}
		</section>
	{/if}
	{#if list}
		<div>
			<Button variant="outline" onclick={sync}>Sync now</Button>
		</div>
	{/if}
	{#if editable || smart || list}
		<div>
			<ConfirmButton
				onconfirm={remove}
				label="Remove this collection"
				title="Remove {data.collection.title}?"
				confirm="Remove collection"
			>
				Its titles stay in the library.
			</ConfirmButton>
		</div>
	{/if}
</div>
