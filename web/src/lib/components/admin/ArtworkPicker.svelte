<script lang="ts">
import { act } from "#lib/act.js";
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
const current = $derived(kinds.find((k) => k.kind === kind) ?? kinds[0]);

function ask(of: Kind) {
	return api.GET("/api/v1/admin/titles/{id}/artwork/candidates", {
		params: { path: { id: title.id }, query: { kind: of } },
	});
}
// The providers' pictures of the kind shown, asked again as the kind
// changes, and after a choice refused: a refresh in between listed them
// afresh, so what is there now is shown.
let listed = $state.raw(ask("poster"));

async function choose(id: string) {
	const chosen = await act(
		api.PUT("/api/v1/admin/titles/{id}/artwork/{artwork}", {
			params: { path: { id: title.id, artwork: kind } },
			body: { id },
		}),
		"Chosen.",
	);
	if (!chosen) listed = ask(kind);
}

const giveBack = () =>
	act(
		api.DELETE("/api/v1/admin/titles/{id}/artwork/{artwork}", {
			params: { path: { id: title.id, artwork: kind } },
		}),
		"Given back to the sources.",
	);
</script>

<Tabs.Root
	bind:value={kind}
	onValueChange={(k) => {
		listed = ask(k as Kind);
	}}
>
	<Tabs.List>
		{#each kinds as k (k.kind)}
			<Tabs.Trigger value={k.kind}>{k.label}</Tabs.Trigger>
		{/each}
	</Tabs.List>
	<Tabs.Content value={kind} class="grid gap-4 pt-2">
		{#await listed}
			<p class="text-ink-3 text-sm">Asking the providers…</p>
		{:then { data, error }}
			{#if !data}
				<p role="alert" class="text-destructive text-sm">
					{problemMessage(error)}
				</p>
			{:else if data.items.length}
				<ul
					class={[
						"grid gap-3",
						kind === "poster"
							? "grid-cols-3 sm:grid-cols-4 lg:grid-cols-6"
							: "grid-cols-1 sm:grid-cols-2 lg:grid-cols-3",
					]}
				>
					{#each data.items as candidate (candidate.id)}
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
					The providers have no {current.label.toLowerCase()} for this title.
				</p>
			{/if}
		{/await}
		<Button variant="outline" class="justify-self-start" onclick={giveBack}>
			Give the {current.label.toLowerCase()} back to the sources
		</Button>
	</Tabs.Content>
</Tabs.Root>
