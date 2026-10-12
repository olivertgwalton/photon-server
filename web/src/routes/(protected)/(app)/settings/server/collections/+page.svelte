<script lang="ts">
import { act } from "#lib/act.js";
import { client } from "#lib/api/client.js";
import ListBrowser from "#lib/components/admin/ListBrowser.svelte";
import Choice from "#lib/components/Choice.svelte";
import PageHeader from "#lib/components/PageHeader.svelte";
import { Badge } from "#lib/components/ui/badge/index.js";
import { Button } from "#lib/components/ui/button/index.js";
import * as Field from "#lib/components/ui/field/index.js";
import { Input } from "#lib/components/ui/input/index.js";
import { fields, text, textAsTyped } from "#lib/form.js";

let { data } = $props();

// Where a collection's titles come from: put in by hand, or a list kept on a
// provider, as Kometa's list builders read one.
const sources = $derived([
	{ value: "hand", label: "By hand" },
	{ value: "tmdb", label: "A TMDB list" },
	{ value: "mdblist", label: "An MDBList list" },
	...data.listers.map((p) => ({
		value: String(p.provider),
		label: `A ${p.name} catalog`,
	})),
]);
let source = $state("hand");
const placeholders: Record<string, string> = {
	tmdb: "8136",
	mdblist: "user/list, or its id",
};

async function add(event: SubmitEvent) {
	const form = fields(event);
	await act(
		client().POST("/api/v1/admin/collections", {
			body: {
				title: textAsTyped(form, "title"),
				library_ids:
					source === "hand"
						? undefined
						: readFrom(textAsTyped(form, "library")),
				list:
					source === "hand" ? undefined : { source, id: text(form, "list") },
			},
		}),
		undefined,
		(made) => `/settings/server/collections/${made.id}`,
	);
}

const origins = {
	user: "Made here",
	smart: "Smart",
	list: "From a list",
	tmdb: "From TMDB",
};

// Where a list's titles are found: every library, or one.
const every = "every";
const libraries = $derived([
	{ value: every, label: "Every library" },
	...data.libraries.map((l) => ({ value: l.id, label: l.name })),
]);

function readFrom(choice: string) {
	return choice === every ? data.libraries.map((l) => l.id) : [choice];
}
</script>

<PageHeader
	title="Collections"
	description="Box sets of titles from any library, listed in a Collections view of their own and in each library whose titles they hold. The providers make some as they match films; those follow the provider and are only read here. Ones made here are yours to fill and order, with films, shows, seasons and episodes alike. A smart one is saved from a library's filters, and holds what they find. One made from a TMDB or MDBList list holds the titles of it your libraries have, read again daily."
/>

{#if data.libraries.length}
	<form onsubmit={add} class="max-w-2xl">
		<Field.Group>
			<div class="grid gap-4 sm:grid-cols-[1fr_auto] sm:items-end">
				<Field.Field>
					<Field.Label for="collection-title">New collection</Field.Label>
					<Input
						id="collection-title"
						name="title"
						required
						placeholder="Christmas films"
					/>
				</Field.Field>
				<div class="flex gap-2">
					<Button type="submit">Make it</Button>
					<ListBrowser libraries={data.libraries.map((l) => l.id)} />
				</div>
			</div>
			<div class="grid gap-4 sm:grid-cols-[auto_1fr] sm:items-end">
				<Field.Field>
					<Field.Label for="collection-source">Titles</Field.Label>
					<Choice
						id="collection-source"
						name="source"
						bind:value={source}
						options={sources}
						class="w-44"
					/>
				</Field.Field>
				{#if source !== "hand"}
					<Field.Field>
						<Field.Label for="collection-list">List</Field.Label>
						<Input
							id="collection-list"
							name="list"
							required
							placeholder={placeholders[source] ?? "movie/top"}
							class="font-mono"
						/>
					</Field.Field>
					<Field.Field>
						<Field.Label for="collection-library">Found in</Field.Label>
						<Choice
							id="collection-library"
							name="library"
							value={every}
							options={libraries}
							class="w-44"
						/>
					</Field.Field>
				{/if}
			</div>
		</Field.Group>
	</form>
{/if}

{#if data.collections.length}
	<ul class="grid gap-2 sm:grid-cols-2 lg:grid-cols-3" aria-label="Collections">
		{#each data.collections as collection (collection.id)}
			<li>
				<a
					href="/settings/server/collections/{collection.id}"
					class="bg-raise hover:ring-line-strong flex items-center justify-between gap-3 rounded-xl p-4 ring-2 ring-transparent"
				>
					<span class="text-ink truncate font-semibold"
						>{collection.title}</span
					>
					<Badge
						variant={collection.origin === "tmdb" ? "outline" : "secondary"}
					>
						{origins[collection.origin ?? "user"]}
					</Badge>
				</a>
			</li>
		{/each}
	</ul>
{:else if data.libraries.length}
	<p class="text-ink-3 text-sm">No collections.</p>
{:else}
	<p class="text-ink-3 text-sm">Add a library first.</p>
{/if}
