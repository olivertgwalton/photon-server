<script lang="ts">
import { act } from "#lib/admin/act.js";
import { fields } from "#lib/form.js";
import { client } from "#lib/api/client.js";
import type { components } from "#lib/api/schema.js";
import ArtworkPicker from "#lib/components/admin/ArtworkPicker.svelte";
import Choice from "#lib/components/admin/Choice.svelte";
import IdentifyPanel from "#lib/components/admin/IdentifyPanel.svelte";
import MarkersEditor from "#lib/components/admin/MarkersEditor.svelte";
import MetadataForm from "#lib/components/admin/MetadataForm.svelte";
import { Button } from "#lib/components/ui/button/index.js";
import * as Card from "#lib/components/ui/card/index.js";
import * as Field from "#lib/components/ui/field/index.js";

let { data } = $props();

const api = client();
const path = $derived({ params: { path: { id: data.title.id } } });

function refresh(mode: components["schemas"]["RefreshMode"]) {
	return act(
		api.POST("/api/v1/admin/titles/{id}/refresh", { ...path, body: { mode } }),
		mode === "all"
			? "Asking the providers about everything again."
			: "Asking the providers for what is missing.",
	);
}

function order(event: SubmitEvent) {
	const asked = String(fields(event).get("order"));
	return act(
		api.PUT("/api/v1/admin/titles/{id}/episode-order", {
			...path,
			body: { order: asked as components["schemas"]["EpisodeOrder"] },
		}),
		"Its episodes are being matched again in that order.",
	);
}

const t = $derived(data.title);
const matched = $derived(t.kind === "movie" || t.kind === "show");
const refreshes = $derived(t.kind !== "collection" && t.kind !== "extra");
const versions = $derived(
	t.kind === "movie" || t.kind === "episode" ? (t.versions ?? []) : [],
);
const name = $derived(
	t.kind === "episode" && t.show
		? `${t.show.title} S${t.season_number} E${t.episode_number} · ${t.title}`
		: t.title,
);
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
					Ask its providers again now, ahead of the library's schedule. A show
					takes its seasons and episodes with it.
				</Card.Description>
			</Card.Header>
			<Card.Content>
				<div class="flex flex-wrap gap-2">
					<Button variant="outline" onclick={() => refresh("missing")}>
						Fill in what's missing
					</Button>
					<Button variant="outline" onclick={() => refresh("all")}>
						Replace everything
					</Button>
				</div>
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

	{#if t.kind === "show"}
		<Card.Root>
			<Card.Header>
				<Card.Title><h2 class="heading">Episode order</h2></Card.Title>
				<Card.Description>
					The order its files are numbered in. Changing it matches every episode
					again.
				</Card.Description>
			</Card.Header>
			<Card.Content>
				<form onsubmit={order} class="flex flex-wrap items-end gap-2">
					<Field.Field class="w-auto">
						<Field.Label for="order">Numbered as</Field.Label>
						<Choice
							id="order"
							name="order"
							value={t.episode_order ?? "aired"}
							options={[
								{ value: "aired", label: "Aired" },
								{ value: "dvd", label: "DVD" },
								{ value: "absolute", label: "Absolute" },
							]}
							class="w-40"
						/>
					</Field.Field>
					<Button type="submit">Save order</Button>
				</form>
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
