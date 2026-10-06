<script lang="ts">
import { toast } from "svelte-sonner";
import { goto } from "$app/navigation";
import { fields } from "#lib/admin/act.js";
import { client } from "#lib/api/client.js";
import { problemMessage } from "#lib/api/problem.js";
import Choice from "#lib/components/admin/Choice.svelte";
import { Badge } from "#lib/components/ui/badge/index.js";
import { Button } from "#lib/components/ui/button/index.js";
import * as Field from "#lib/components/ui/field/index.js";
import { Input } from "#lib/components/ui/input/index.js";

let { data } = $props();

async function add(event: SubmitEvent) {
	const form = fields(event);
	const { data: made, error } = await client().POST(
		"/api/v1/admin/collections",
		{
			body: {
				library_id: String(form.get("library") ?? ""),
				title: String(form.get("title") ?? ""),
			},
		},
	);
	if (error) return toast.error(problemMessage(error));
	await goto(`/admin/collections/${made.id}`);
}

const libraries = $derived(
	data.shelves.map((s) => ({ value: s.library.id, label: s.library.name })),
);
</script>

<svelte:head><title>Collections · Dashboard · Photon</title></svelte:head>

<h1 class="title">Collections</h1>
<p class="max-w-2xl text-sm">
	Box sets of a library's titles. The providers make some as they match films;
	those follow the provider and are only read here. Ones made here are yours to
	fill and order.
</p>

{#if libraries.length}
	<form onsubmit={add} class="max-w-2xl">
		<Field.Group>
			<div class="grid gap-4 sm:grid-cols-[1fr_auto_auto] sm:items-end">
				<Field.Field>
					<Field.Label for="collection-title">New collection</Field.Label>
					<Input
						id="collection-title"
						name="title"
						required
						placeholder="Christmas films"
					/>
				</Field.Field>
				<Field.Field>
					<Field.Label for="collection-library">In</Field.Label>
					<Choice
						id="collection-library"
						name="library"
						value={libraries[0]?.value}
						options={libraries}
						class="w-40"
					/>
				</Field.Field>
				<Button type="submit">Make it</Button>
			</div>
		</Field.Group>
	</form>
{/if}

{#each data.shelves as shelf (shelf.library.id)}
	<section aria-labelledby="shelf-{shelf.library.id}" class="grid gap-3">
		<h2 id="shelf-{shelf.library.id}" class="heading">{shelf.library.name}</h2>
		{#if shelf.collections.length}
			<ul class="grid gap-2 sm:grid-cols-2 lg:grid-cols-3">
				{#each shelf.collections as collection (collection.id)}
					<li>
						<a
							href="/admin/collections/{collection.id}"
							class="bg-raise hover:ring-line-strong flex items-center justify-between gap-3 rounded-xl p-4 ring-2 ring-transparent"
						>
							<span class="text-ink truncate font-semibold"
								>{collection.title}</span
							>
							<Badge
								variant={collection.origin === "user" ? "secondary" : "outline"}
							>
								{collection.origin === "user" ? "Made here" : "From TMDB"}
							</Badge>
						</a>
					</li>
				{/each}
			</ul>
		{:else}
			<p class="text-ink-3 text-sm">No collections.</p>
		{/if}
	</section>
{:else}
	<p class="text-ink-3 text-sm">Add a library first.</p>
{/each}
