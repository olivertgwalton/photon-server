<script lang="ts">
import ListMusicIcon from "@lucide/svelte/icons/list-music";
import MicVocalIcon from "@lucide/svelte/icons/mic-vocal";
import { blurStyle, tintColour } from "#lib/blurhash.ts";
import Artwork from "#lib/components/Artwork.svelte";
import { Button } from "#lib/components/ui/button/index.ts";
import * as Sheet from "#lib/components/ui/sheet/index.ts";
import { Toggle } from "#lib/components/ui/toggle/index.ts";
import { timecode } from "#lib/format.ts";
import { chapterAt } from "#lib/player/listen.ts";
import { listening } from "#lib/player/listening.svelte.ts";
import Lyrics from "./Lyrics.svelte";
import PlayerControls from "./PlayerControls.svelte";
import PlayerSettings from "./PlayerSettings.svelte";
import SeekBar from "./SeekBar.svelte";

// What plays, large, over its cover's colour, as Plexamp's and Finamp's Now
// Playing screens are.
let {
	open = $bindable(false),
	onqueue,
}: { open: boolean; onqueue: () => void } = $props();

const item = $derived(listening.current);
const chapter = $derived(chapterAt(listening.position, item?.chapters));
const tint = $derived(tintColour(item?.blurhash) ?? "transparent");
// A track's lyrics in its cover's place; an audiobook has chapters instead.
const sung = $derived(!!item && !item.chapters?.length);
let lyrics = $state(false);
</script>

<Sheet.Root bind:open>
	<Sheet.Content
		side="bottom"
		class="bg-ground h-svh overflow-y-auto border-none"
		style="--tint: {tint}"
	>
		<div
			class="pointer-events-none absolute inset-0 bg-linear-to-b from-(--tint) via-(--tint)/40 to-transparent"
			aria-hidden="true"
		></div>
		{#if item}
			<div
				class="relative mx-auto grid w-full max-w-md content-center gap-6 px-6 py-10 sm:min-h-full"
			>
				<Sheet.Header class="p-0">
					<Sheet.Title class="sr-only">Now playing</Sheet.Title>
				</Sheet.Header>
				{#if sung && lyrics}
					<div class="aspect-square w-full">
						<Lyrics id={item.id} />
					</div>
				{:else}
					<div
						class="bg-raise aspect-square w-full overflow-hidden rounded-xl shadow-2xl"
						style={blurStyle(item.blurhash)}
					>
						{#if item.artwork}
							{#key item.artwork}
								<Artwork
									id={item.artwork}
									shape="square"
									loading="eager"
									sizes="28rem"
									class="size-full object-cover"
								/>
							{/key}
						{/if}
					</div>
				{/if}
				<div class="grid gap-1">
					<p class="title text-ink text-2xl">{item.title}</p>
					{#if chapter?.title}
						<p class="text-ink-2">{chapter.title}</p>
					{/if}
					<p class="text-ink-2">
						{item.by ?? ""}
						{#if item.from}
							·
							<a
								href="/titles/{item.from.id}"
								class="hover:underline"
								onclick={() => (open = false)}
							>
								{item.from.title}
							</a>
						{/if}
					</p>
				</div>
				<div class="grid gap-1">
					<SeekBar
						position={listening.position}
						duration={listening.duration}
						buffered={listening.buffered}
						chapters={item.chapters ?? []}
						onseek={listening.seek}
					/>
					<div
						class="text-ink-3 flex justify-between font-mono text-xs tabular-nums"
					>
						<span>{timecode(listening.position * 1000)}</span>
						<span>
							-{timecode((listening.duration - listening.position) * 1000)}
						</span>
					</div>
				</div>
				<div class="flex justify-center">
					<PlayerControls size="large" />
				</div>
				<div class="flex justify-center gap-2">
					<Button variant="ghost" onclick={onqueue}>
						<ListMusicIcon />Queue
					</Button>
					{#if sung}
						<Toggle bind:pressed={lyrics} aria-label="Lyrics">
							<MicVocalIcon />Lyrics
						</Toggle>
					{/if}
					<PlayerSettings />
				</div>
			</div>
		{/if}
	</Sheet.Content>
</Sheet.Root>
