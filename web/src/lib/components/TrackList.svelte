<script lang="ts">
import AudioLinesIcon from "@lucide/svelte/icons/audio-lines";
import PlayIcon from "@lucide/svelte/icons/play";
import type { components } from "#lib/api/schema.d.ts";
import * as Table from "#lib/components/ui/table/index.ts";
import { timecode } from "#lib/format.ts";
import { discs, guests } from "#lib/music.ts";
import { listening } from "#lib/player/listening.svelte.ts";

type Card = components["schemas"]["Card"];

// An album's tracks, disc by disc where it has more than one, each played
// from by choosing it, the rest of the album queued after it.
let {
	tracks,
	artist,
	onplay,
}: {
	tracks: Card[];
	artist?: string;
	onplay: (track: Card) => void;
} = $props();

const split = $derived(discs(tracks));
</script>

<Table.Root class="table-fixed">
	<colgroup>
		<col class="w-12">
		<col>
		<col class="w-20">
	</colgroup>
	<Table.Header class="sr-only">
		<Table.Row>
			<Table.Head>Number</Table.Head>
			<Table.Head>Title</Table.Head>
			<Table.Head>Length</Table.Head>
		</Table.Row>
	</Table.Header>
	{#each split as { disc, tracks: on } (disc)}
		<Table.Body>
			{#if split.length > 1}
				<Table.Row class="hover:bg-transparent">
					<Table.Cell colspan={3} class="text-ink-3 pt-6 text-sm font-semibold">
						Disc {disc}
					</Table.Cell>
				</Table.Row>
			{/if}
			{#each on as track (track.id)}
				{@const playing = listening.current?.id === track.id}
				{@const also = guests(track, artist)}
				<Table.Row class="group border-line/50 relative">
					<Table.Cell class="text-ink-3 w-12 text-right font-mono tabular-nums">
						{#if playing}
							<AudioLinesIcon
								class="text-signal ml-auto size-4"
								aria-label="Playing"
							/>
						{:else}
							<span class="group-hover:hidden">{track.track_number ?? ""}</span>
							<PlayIcon
								class="ml-auto hidden size-4 fill-current group-hover:block"
								aria-hidden="true"
							/>
						{/if}
					</Table.Cell>
					<Table.Cell class="whitespace-normal">
						<button
							type="button"
							class={[
								"block w-full truncate text-left outline-none after:absolute after:inset-0 focus-visible:underline",
								playing ? "text-signal" : "text-ink",
							]}
							onclick={() => onplay(track)}
						>
							{track.title}
						</button>
						{#if also}
							<span class="text-ink-3 block truncate text-xs">{also}</span>
						{/if}
					</Table.Cell>
					<Table.Cell class="text-ink-3 w-20 text-right font-mono tabular-nums">
						{track.duration_ms ? timecode(track.duration_ms) : ""}
					</Table.Cell>
				</Table.Row>
			{/each}
		</Table.Body>
	{/each}
</Table.Root>
