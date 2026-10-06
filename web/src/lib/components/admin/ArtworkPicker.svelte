<script lang="ts">
import { toast } from "svelte-sonner";
import { invalidateAll } from "$app/navigation";
import { client } from "#lib/api/client.js";
import { problemMessage } from "#lib/api/problem.js";
import type { components } from "#lib/api/schema.js";
import { artworkURL } from "#lib/artwork.js";
import { Button } from "#lib/components/ui/button/index.js";
import * as Tabs from "#lib/components/ui/tabs/index.js";

const api = client();

type Schemas = components["schemas"];
type Kind = Schemas["ArtworkKind"];

// Choosing a title's pictures from what its providers have, as Jellyfin's
// Edit Images and Plex's poster chooser do. A choice holds through every
// refresh until it is given back.
let { title }: { title: Schemas["TitlePage"] } = $props();

const kinds: { kind: Kind; label: string; shape: string; width: number }[] = [
	{ kind: "poster", label: "Poster", shape: "aspect-[2/3]", width: 240 },
	{ kind: "backdrop", label: "Backdrop", shape: "aspect-video", width: 480 },
	{ kind: "thumb", label: "Still", shape: "aspect-video", width: 480 },
	{ kind: "logo", label: "Logo", shape: "aspect-[5/2]", width: 480 },
	{ kind: "banner", label: "Banner", shape: "aspect-[27/5]", width: 640 },
];

let kind = $state<Kind>("poster");
let candidates = $state<Schemas["ArtworkCandidate"][]>();
let refusal = $state("");

async function load(of: Kind) {
	candidates = undefined;
	const { data, error } = await api.GET(
		"/api/v1/admin/titles/{id}/artwork/candidates",
		{
			params: { path: { id: title.id }, query: { kind: of } },
		},
	);
	refusal = error ? problemMessage(error) : "";
	if (of === kind) candidates = data?.items ?? [];
}

// Asked again whenever the title is, since a refresh lists its pictures afresh.
$effect(() => {
	load(kind);
});

async function choose(id: string) {
	const { error } = await api.PUT("/api/v1/admin/titles/{id}/artwork/{kind}", {
		params: { path: { id: title.id, kind } },
		body: { id },
	});
	if (error) {
		toast.error(problemMessage(error));
		// A refresh in between lists the pictures afresh; show what is there now.
		return load(kind);
	}
	toast.success("Chosen.");
	await invalidateAll();
}

async function giveBack() {
	const { error } = await api.DELETE(
		"/api/v1/admin/titles/{id}/artwork/{kind}",
		{
			params: { path: { id: title.id, kind } },
		},
	);
	if (error) return toast.error(problemMessage(error));
	toast.success("Given back to the sources.");
	await invalidateAll();
}

const current = $derived(kinds.find((k) => k.kind === kind) ?? kinds[0]);
</script>

<Tabs.Root bind:value={kind}>
	<Tabs.List>
		{#each kinds as k (k.kind)}
			<Tabs.Trigger value={k.kind}>{k.label}</Tabs.Trigger>
		{/each}
	</Tabs.List>
	{#each kinds as k (k.kind)}
		<Tabs.Content value={k.kind} class="grid gap-4 pt-2">
			{#if refusal}
				<p role="alert" class="text-destructive text-sm">{refusal}</p>
			{:else if !candidates}
				<p class="text-ink-3 text-sm">Asking the providers…</p>
			{:else if candidates.length}
				<ul
					class={[
						"grid gap-3",
						k.kind === "poster"
							? "grid-cols-3 sm:grid-cols-4 lg:grid-cols-6"
							: "grid-cols-1 sm:grid-cols-2 lg:grid-cols-3",
					]}
				>
					{#each candidates as candidate (candidate.id)}
						<li>
							<button
								type="button"
								onclick={() => choose(candidate.id)}
								aria-pressed={candidate.chosen}
								class={[
									"bg-ground group block w-full overflow-hidden rounded-lg ring-2 outline-none focus-visible:ring-signal",
									candidate.chosen
										? "ring-signal"
										: "hover:ring-line-strong ring-transparent",
								]}
							>
								<img
									src={artworkURL(candidate.id, current.width)}
									alt="{current.label} from {candidate.source}{candidate.language
										? `, ${candidate.language}`
										: ""}"
									loading="lazy"
									class={["w-full object-contain", current.shape]}
								>
								<span class="text-ink-3 block px-2 py-1 text-left text-xs">
									{candidate.source}{candidate.language
										? ` · ${candidate.language}`
										: ""}
									{candidate.width && candidate.height
										? ` · ${candidate.width}×${candidate.height}`
										: ""}
									{candidate.chosen ? " · chosen" : ""}
								</span>
							</button>
						</li>
					{/each}
				</ul>
			{:else}
				<p class="text-ink-3 text-sm">
					The providers have no {k.label.toLowerCase()} for this title.
				</p>
			{/if}
			<Button variant="outline" class="justify-self-start" onclick={giveBack}>
				Give the {k.label.toLowerCase()} back to the sources
			</Button>
		</Tabs.Content>
	{/each}
</Tabs.Root>
