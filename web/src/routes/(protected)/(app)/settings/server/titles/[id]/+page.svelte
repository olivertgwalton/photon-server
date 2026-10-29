<script lang="ts">
import { act } from "#lib/act.js";
import { mergeTitle } from "#lib/actions.js";
import { matchable, refreshable } from "#lib/admin/edit.js";
import { client } from "#lib/api/client.js";
import type { components } from "#lib/api/schema.js";
import ArtworkPicker from "#lib/components/admin/ArtworkPicker.svelte";
import ConfirmButton from "#lib/components/admin/ConfirmButton.svelte";
import IdentifyPanel from "#lib/components/admin/IdentifyPanel.svelte";
import MarkersEditor from "#lib/components/admin/MarkersEditor.svelte";
import MetadataForm from "#lib/components/admin/MetadataForm.svelte";
import TitleLocale from "#lib/components/admin/TitleLocale.svelte";
import Choice from "#lib/components/Choice.svelte";
import { Button } from "#lib/components/ui/button/index.js";
import * as Card from "#lib/components/ui/card/index.js";
import * as Field from "#lib/components/ui/field/index.js";
import { Input } from "#lib/components/ui/input/index.js";
import { fields } from "#lib/form.js";
import { fullTitle } from "#lib/format.js";
import { getShell } from "#lib/shell.svelte.js";

let { data } = $props();
const shell = getShell();

const api = client();
const path = $derived({ params: { path: { id: data.title.id } } });

// An episode group is chosen as "group:" and its id, beside the orders.
function order(event: SubmitEvent) {
	const asked = String(fields(event).get("order"));
	const group = asked.startsWith("group:") ? asked.slice(6) : undefined;
	return act(
		api.PUT("/api/v1/admin/titles/{id}/episode-order", {
			...path,
			body: group
				? { order: "group", group }
				: { order: asked as components["schemas"]["EpisodeOrder"] },
		}),
		"Its episodes are being matched again in that order.",
	);
}

const kinds: Record<components["schemas"]["GroupKind"], string> = {
	aired: "Aired",
	absolute: "Absolute",
	dvd: "DVD",
	digital: "Digital",
	story_arc: "Story arcs",
	production: "Production",
	tv: "TV",
};

const orders = $derived([
	{ value: "aired", label: "Aired" },
	{ value: "dvd", label: "DVD" },
	{ value: "absolute", label: "Absolute" },
	...data.groups.map((g) => ({
		value: `group:${g.id}`,
		label: `${g.name} (${kinds[g.kind]}, ${g.seasons} ${g.seasons === 1 ? "season" : "seasons"})`,
	})),
]);

const t = $derived(data.title);
const matched = $derived(matchable(t.kind));
const refreshes = $derived(refreshable(t.kind));
const versions = $derived(
	t.kind === "movie" || t.kind === "episode" ? (t.versions ?? []) : [],
);
const name = $derived(fullTitle(t, t.show?.title));

// Another film of its library to merge into it, found by name.
let query = $state("");
let found = $state.raw<components["schemas"]["Card"][]>([]);

async function search(event: SubmitEvent) {
	event.preventDefault();
	const { data: answer } = await api.GET("/api/v1/search", {
		params: {
			query: {
				q: query,
				kind: ["movie"],
				library: t.library_id ?? undefined,
				limit: 12,
			},
		},
	});
	found = (answer?.titles.items ?? []).filter((c) => c.id !== t.id);
}

async function merge(other: components["schemas"]["Card"]) {
	if (await mergeTitle(t.id, other.id, caption(other))) {
		found = found.filter((c) => c.id !== other.id);
	}
}

function caption(card: components["schemas"]["Card"]) {
	return card.year ? `${card.title} (${card.year})` : card.title;
}
</script>

<svelte:head><title>Edit {t.title} · Settings · Photon</title></svelte:head>

<div class="grid max-w-3xl gap-6">
	<div>
		<p class="label">Editing</p>
		<h1 class="title">
			<a href="/titles/{t.id}" class="hover:underline">{name}</a>
		</h1>
	</div>

	{#if refreshes}
		<Card.Root id="refresh" class="scroll-mt-20">
			<Card.Header>
				<Card.Title><h2 class="heading">Refresh</h2></Card.Title>
				<Card.Description>
					Update it from its providers now, without waiting for the library's
					schedule.
				</Card.Description>
			</Card.Header>
			<Card.Content>
				<Button
					variant="outline"
					onclick={() => shell.refreshMetadata("title", t.id, name)}
				>
					Refresh metadata…
				</Button>
			</Card.Content>
		</Card.Root>
	{/if}

	{#if matched}
		<Card.Root id="identify" class="scroll-mt-20">
			<Card.Header>
				<Card.Title><h2 class="heading">Match</h2></Card.Title>
				<Card.Description>
					{#if t.ids && Object.keys(t.ids).length}
						Matched to
						{Object.entries(t.ids)
							.map(([provider, id]) => `${provider} ${id}`)
							.join(", ")}.
					{:else}
						Not matched to any provider.
					{/if}
				</Card.Description>
			</Card.Header>
			<Card.Content class="grid gap-4">
				<IdentifyPanel title={t} providers={data.providers} />
			</Card.Content>
		</Card.Root>
	{/if}

	{#if matched}
		<Card.Root>
			<Card.Header>
				<Card.Title><h2 class="heading">Language</h2></Card.Title>
				<Card.Description>
					What its metadata is asked in, over its library's, its seasons and
					episodes with it.
				</Card.Description>
			</Card.Header>
			<Card.Content>
				<TitleLocale title={t} locales={data.locales} />
			</Card.Content>
		</Card.Root>
	{/if}

	{#if t.kind === "show"}
		<Card.Root>
			<Card.Header>
				<Card.Title><h2 class="heading">Episode order</h2></Card.Title>
				<Card.Description>
					The order its files are numbered in, or one of the episode groups TMDB
					has it in. Changing it matches every episode again.
				</Card.Description>
			</Card.Header>
			<Card.Content>
				<form onsubmit={order} class="flex flex-wrap items-end gap-2">
					<Field.Field class="w-auto">
						<Field.Label for="order">Numbered as</Field.Label>
						<Choice
							id="order"
							name="order"
							value={t.episode_order === "group"
								? `group:${t.episode_group}`
								: (t.episode_order ?? "aired")}
							options={orders}
							class="w-72"
						/>
					</Field.Field>
					<Button type="submit">Save order</Button>
				</form>
			</Card.Content>
		</Card.Root>
	{/if}

	{#if t.kind === "movie"}
		<Card.Root id="versions" class="scroll-mt-20">
			<Card.Header>
				<Card.Title><h2 class="heading">Versions</h2></Card.Title>
				<Card.Description>
					Merge another film of its library into it, and its versions become
					this film's, and stay so through every scan. What anyone has watched
					of it, favourited or listed comes with them.
				</Card.Description>
			</Card.Header>
			<Card.Content class="grid gap-3">
				<form onsubmit={search} class="flex gap-2">
					<Input
						bind:value={query}
						type="search"
						aria-label="Find a film"
						placeholder="Find a film"
						required
					/>
					<Button type="submit" variant="outline">Find</Button>
				</form>
				{#if found.length}
					<ul class="grid gap-1">
						{#each found as card (card.id)}
							<li class="flex items-center justify-between gap-3 text-sm">
								<span class="text-ink truncate">{caption(card)}</span>
								<ConfirmButton
									label="Merge in"
									hidden={card.title}
									title="Merge {caption(card)} into {name}?"
									body="Its versions become this film's, and it leaves the library. Split apart parts them again."
									confirm="Merge"
									onconfirm={() => merge(card)}
								/>
							</li>
						{/each}
					</ul>
				{/if}
			</Card.Content>
		</Card.Root>
	{/if}

	<Card.Root id="edit" class="scroll-mt-20">
		<Card.Header>
			<Card.Title><h2 class="heading">Details</h2></Card.Title>
			<Card.Description>
				What is written here outranks every source and lasts through refreshes.
				Hold a field to keep it as it is without changing it.
			</Card.Description>
		</Card.Header>
		<Card.Content>
			{#key t}
				<MetadataForm title={t} />
			{/key}
		</Card.Content>
	</Card.Root>

	{#if t.kind !== "extra"}
		<Card.Root id="artwork" class="scroll-mt-20">
			<Card.Header>
				<Card.Title><h2 class="heading">Artwork</h2></Card.Title>
			</Card.Header>
			<Card.Content>
				<ArtworkPicker title={t} />
			</Card.Content>
		</Card.Root>
	{/if}

	{#each versions as version, i (version.id)}
		<Card.Root id={i ? undefined : "markers"} class="scroll-mt-20">
			<Card.Header>
				<Card.Title>
					<h2 class="heading">
						Markers{versions.length > 1
							? `: ${version.label ?? version.edition ?? version.container}`
							: ""}
					</h2>
				</Card.Title>
			</Card.Header>
			<Card.Content>
				{#key version}
					<MarkersEditor {version} />
				{/key}
			</Card.Content>
		</Card.Root>
	{/each}
</div>
