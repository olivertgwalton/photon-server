<script lang="ts">
import { untrack } from "svelte";
import type { Attachment } from "svelte/attachments";
import { client } from "#lib/api/client.ts";
import type { components } from "#lib/api/schema.d.ts";
import { lineAt } from "#lib/player/listen.ts";
import { listening } from "#lib/player/listening.svelte.ts";

type Lyrics = components["schemas"]["Lyrics"];

// A track's lyrics, as Plexamp and Finamp show them: timed ones follow the
// track, the line sung lit and kept in the middle, and choosing one plays
// from it.
let { id }: { id: string } = $props();

async function load(id: string): Promise<Lyrics | null> {
	const { data, response } = await client().GET("/api/v1/titles/{id}/lyrics", {
		params: { path: { id } },
	});
	if (response.status === 404) return null;
	if (!data) throw new Error(`lyrics: ${response.status}`);
	return data;
}

const lyrics = $derived(load(id));

// Lyrics are timed where their lines say where they are sung.
function timed(l: Lyrics): boolean {
	return l.lines.some((line) => line.start_ms != null);
}
let at = $state(-1);

// Follows the track as it plays, and scrolls only when the line changes.
function follow(l: Lyrics): Attachment<HTMLOListElement> {
	return (list) => {
		if (!timed(l)) return;
		const next = lineAt(l.lines, listening.position * 1000);
		if (next === untrack(() => at)) return;
		at = next;
		list.children[next]?.scrollIntoView({
			block: "center",
			behavior: "smooth",
		});
	};
}
</script>

{#await lyrics}
	<p class="text-ink-3 grid h-full place-items-center">Looking for lyrics…</p>
{:then l}
	{#if l}
		<ol
			{@attach follow(l)}
			class="grid h-full content-start gap-3 overflow-y-auto px-2 py-[40%] text-xl font-semibold"
		>
			{#each l.lines as line, i (i)}
				<li>
					{#if line.start_ms != null}
						{@const start = line.start_ms}
						<button
							type="button"
							class={[
								"text-left transition-colors outline-none hover:text-ink focus-visible:underline",
								i === at ? "text-ink" : "text-ink-3",
							]}
							onclick={() => listening.seek(start / 1000)}
						>
							{line.text || "♪"}
						</button>
					{:else}
						<span class="text-ink-2">{line.text}</span>
					{/if}
				</li>
			{/each}
		</ol>
	{:else}
		<p class="text-ink-3 grid h-full place-items-center">No lyrics</p>
	{/if}
{:catch}
	<p class="text-ink-3 grid h-full place-items-center">
		The lyrics couldn't be read.
	</p>
{/await}
