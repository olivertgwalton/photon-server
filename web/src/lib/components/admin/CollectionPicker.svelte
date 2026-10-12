<script lang="ts">
import LibraryBigIcon from "@lucide/svelte/icons/library-big";
import { addToCollection, newCollection } from "#lib/actions.js";
import { client } from "#lib/api/client.js";
import { problemMessage } from "#lib/api/problem.js";
import { Button } from "#lib/components/ui/button/index.js";
import * as Dialog from "#lib/components/ui/dialog/index.js";
import * as Field from "#lib/components/ui/field/index.js";
import { Input } from "#lib/components/ui/input/index.js";
import { fields, text } from "#lib/form.js";
import { getShell } from "#lib/shell.svelte.js";

const asked = getShell().collectionPicker;
const id = $props.id();

// The title's library and the collections of it an admin puts titles in by
// hand: a provider's, a list's or a rule's are theirs to fill. Every page is
// read, as a library may hold hundreds of a provider's box sets.
async function load(title: string) {
	const api = client();
	const { library_id: library } = await answer(
		api.GET("/api/v1/titles/{id}", { params: { path: { id: title } } }),
	);
	if (!library) throw new Error("A collection is put in no collection.");
	const mine = [];
	for (let offset = 0, total = 1; offset < total; offset += 200) {
		const page = await answer(
			api.GET("/api/v1/libraries/{id}/collections", {
				params: { path: { id: library }, query: { offset, limit: 200 } },
			}),
		);
		mine.push(...page.items.filter((c) => c.origin === "user"));
		total = page.total;
	}
	return { library, collections: mine };
}

// What the API answered, or why it refused, said in the dialog.
async function answer<T>(call: Promise<{ data?: T; error?: unknown }>) {
	const { data, error } = await call;
	if (!data) throw new Error(problemMessage(error));
	return data;
}

async function add(collection: string, name: string) {
	if (await addToCollection(collection, [asked.id], name)) asked.open = false;
}

async function create(event: SubmitEvent, library: string) {
	const name = text(fields(event), "name");
	if (await newCollection(library, name, [asked.id])) asked.open = false;
}
</script>

<Dialog.Root bind:open={asked.open}>
	<Dialog.Content>
		<Dialog.Header>
			<Dialog.Title>Add to collection</Dialog.Title>
			<Dialog.Description>{asked.title}</Dialog.Description>
		</Dialog.Header>
		<!-- Asked each time it opens, so a collection made elsewhere is there. -->
		{#if asked.open}
			<svelte:boundary>
				{@const { library, collections } = await load(asked.id)}
				{#if collections.length}
					<ul class="-mx-2 grid max-h-64 overflow-y-auto">
						{#each collections as collection (collection.id)}
							<li>
								<Button
									variant="ghost"
									class="w-full justify-start"
									onclick={() => add(collection.id, collection.title)}
								>
									<LibraryBigIcon />
									{collection.title}
								</Button>
							</li>
						{/each}
					</ul>
				{/if}
				<form onsubmit={(event) => create(event, library)}>
					<Field.Field orientation="horizontal">
						<Field.Label for="{id}-new-collection" class="sr-only">
							New collection
						</Field.Label>
						<Input
							id="{id}-new-collection"
							name="name"
							placeholder="New collection"
							required
						/>
						<Button type="submit">Create</Button>
					</Field.Field>
				</form>
				{#snippet pending()}
					<p class="text-ink-3">Loading collections…</p>
				{/snippet}
				{#snippet failed(
					error: unknown,
				)}
					<p role="alert" class="text-destructive">
						{error instanceof Error ? error.message : problemMessage(error)}
					</p>
				{/snippet}
			</svelte:boundary>
		{/if}
	</Dialog.Content>
</Dialog.Root>
