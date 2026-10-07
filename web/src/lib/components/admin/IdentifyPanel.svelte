<script lang="ts">
import { client } from "#lib/api/client.js";
import { problemMessage } from "#lib/api/problem.js";
import type { components } from "#lib/api/schema.js";
import { Button } from "#lib/components/ui/button/index.js";
import * as Field from "#lib/components/ui/field/index.js";
import { Input } from "#lib/components/ui/input/index.js";
import { act } from "#lib/act.js";
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
				<!-- As Plex's Fix Match lists each: its poster, name and year, and
					what it is about, to tell like-named titles apart. -->
				<li class="flex items-start gap-4 py-3">
					<div
						class="bg-raise aspect-[2/3] w-16 shrink-0 overflow-hidden rounded-md"
					>
						{#if candidate.poster}
							<img
								src={candidate.poster}
								alt=""
								loading="lazy"
								decoding="async"
								class="size-full object-cover"
							>
						{/if}
					</div>
					<div class="grid min-w-0 flex-1 gap-1">
						<p
							class="text-ink flex items-baseline justify-between gap-3 font-semibold"
						>
							<span class="truncate">{candidate.title}</span>
							{#if candidate.year}
								<span
									class="text-ink-3 shrink-0 text-sm font-normal tabular-nums"
								>
									{candidate.year}
								</span>
							{/if}
						</p>
						{#if candidate.original_title &&
							candidate.original_title !== candidate.title}
							<p class="text-ink-3 truncate text-xs">
								{candidate.original_title}
							</p>
						{/if}
						{#if candidate.overview}
							<p class="text-ink-2 line-clamp-3 text-sm">
								{candidate.overview}
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
						Select <span class="sr-only">{candidate.title}</span>
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
