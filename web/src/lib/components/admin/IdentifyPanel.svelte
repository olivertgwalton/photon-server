<script lang="ts">
import { client } from "#lib/api/client.js";
import { problemMessage } from "#lib/api/problem.js";
import type { components } from "#lib/api/schema.js";
import { Button } from "#lib/components/ui/button/index.js";
import * as Field from "#lib/components/ui/field/index.js";
import { Input } from "#lib/components/ui/input/index.js";
import { act } from "#lib/admin/act.js";
import Choice from "./Choice.svelte";

const api = client();

type Schemas = components["schemas"];

// Fixing what a film or show is matched to, as Plex's Fix Match and
// Jellyfin's Identify do: search a provider by a name, and pin the one meant.
let {
	title,
	providers,
}: { title: Schemas["TitlePage"]; providers: Schemas["MetadataProvider"][] } =
	$props();

const searchers = $derived(
	providers
		.filter(
			(p) =>
				p.capabilities.includes("search") &&
				p.kinds.includes(title.kind) &&
				p.ready,
		)
		.map((p) => ({ value: String(p.id), label: p.name })),
);

let provider = $state("");
$effect.pre(() => {
	if (!provider) provider = searchers[0]?.value ?? "";
});
let name = $state("");
let year = $state("");
$effect.pre(() => {
	name = title.title;
	year = title.year ? String(title.year) : "";
});

let candidates = $state<Schemas["Candidate"][]>();
let refusal = $state("");
let searching = $state(false);

async function search(event: SubmitEvent) {
	event.preventDefault();
	searching = true;
	const { data, error } = await api.GET(
		"/api/v1/admin/titles/{id}/candidates",
		{
			params: {
				path: { id: title.id },
				query: { provider, title: name, year: year ? Number(year) : undefined },
			},
		},
	);
	searching = false;
	refusal = error ? problemMessage(error) : "";
	candidates = data?.items;
}
</script>

{#if searchers.length}
	<form onsubmit={search} class="grid gap-4">
		<div class="grid gap-4 sm:grid-cols-[auto_1fr_6rem_auto] sm:items-end">
			<Field.Field>
				<Field.Label for="identify-provider">Provider</Field.Label>
				<Choice
					id="identify-provider"
					name="provider"
					bind:value={provider}
					options={searchers}
				/>
			</Field.Field>
			<Field.Field>
				<Field.Label for="identify-title">Name</Field.Label>
				<Input id="identify-title" bind:value={name} required />
			</Field.Field>
			<Field.Field>
				<Field.Label for="identify-year">Year</Field.Label>
				<Input
					id="identify-year"
					bind:value={year}
					inputmode="numeric"
					maxlength={4}
				/>
			</Field.Field>
			<Button type="submit" variant="outline" disabled={searching}
				>Search</Button
			>
		</div>
		<Field.Error errors={[{ message: refusal }]} />
	</form>
	{#if candidates}
		<ul class="divide-line divide-y">
			{#each candidates as candidate (candidate.id)}
				<li class="flex items-center justify-between gap-4 py-2.5">
					<div class="min-w-0">
						<p class="text-ink truncate font-semibold">
							{candidate.title}{candidate.year ? ` (${candidate.year})` : ""}
						</p>
						{#if candidate.original_title &&
							candidate.original_title !== candidate.title}
							<p class="text-ink-3 truncate text-xs">
								{candidate.original_title}
							</p>
						{/if}
					</div>
					<Button
						size="sm"
						onclick={() =>
							act(
								api.PUT("/api/v1/admin/titles/{id}/match", {
									params: { path: { id: title.id } },
									body: { provider, id: candidate.id },
								}),
								"Matched. Its details follow in a moment.",
							)}
					>
						This one <span class="sr-only">: {candidate.title}</span>
					</Button>
				</li>
			{:else}
				<li class="text-ink-3 py-2.5 text-sm">Nothing by that name.</li>
			{/each}
		</ul>
	{/if}
{:else}
	<p class="text-ink-3 text-sm">
		No provider that searches is ready for this kind of title.
	</p>
{/if}
