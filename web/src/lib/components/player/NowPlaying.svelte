<script lang="ts">
import ListMusicIcon from "@lucide/svelte/icons/list-music";
import MicVocalIcon from "@lucide/svelte/icons/mic-vocal";
import { cubicOut } from "svelte/easing";
import { prefersReducedMotion } from "svelte/motion";
import { fly, scale } from "svelte/transition";
import { blurStyle, tintColour } from "#lib/blurhash.ts";
import Artwork from "#lib/components/Artwork.svelte";
import ExplicitMark from "#lib/components/ExplicitMark.svelte";
import { Button } from "#lib/components/ui/button/index.ts";
import * as Sheet from "#lib/components/ui/sheet/index.ts";
import { Toggle } from "#lib/components/ui/toggle/index.ts";
import { timecode } from "#lib/format.ts";
import { chapterAt } from "#lib/player/listen.ts";
import { listening } from "#lib/player/listening.svelte.ts";
import Lyrics from "./Lyrics.svelte";
import OptionsMenu from "./OptionsMenu.svelte";
import PlayerControls from "./PlayerControls.svelte";
import PlayerSettings from "./PlayerSettings.svelte";
import SeekBar from "./SeekBar.svelte";

// What plays, large, over its cover's colour, as Plexamp's and Finamp's Now
// Playing screens are.
let {
	open = $bindable(false),
	lyrics = $bindable(false),
	onqueue,
}: { open: boolean; lyrics: boolean; onqueue: () => void } = $props();

const item = $derived(listening.current);
const chapter = $derived(chapterAt(listening.position, item?.chapters));
const tint = $derived(tintColour(item?.blurhash) ?? "transparent");
// A track's lyrics in its cover's place, where it has any; an audiobook has
// chapters instead.
const sung = $derived(item?.kind === "track" && !!item.lyrics);
// The cover sinks back as the lyrics rise over it, as Apple Music's do, both
// in the one square so nothing below them moves.
const swap = $derived({
	duration: prefersReducedMotion.current ? 0 : 280,
	easing: cubicOut,
});
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
				<div
					class="grid aspect-square min-h-0 w-full grid-cols-1 grid-rows-[minmax(0,1fr)] *:col-start-1 *:row-start-1"
				>
					{#if sung && lyrics}
						<div class="size-full" transition:fly={{ ...swap, y: 24 }}>
							<Lyrics id={item.id} version={item.version} />
						</div>
					{:else}
						<div
							class="bg-raise size-full overflow-hidden rounded-xl shadow-2xl"
							style={blurStyle(item.blurhash)}
							transition:scale={{ ...swap, start: 0.9 }}
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
				</div>
				<div class="grid gap-1">
					<p class="title text-ink flex items-center gap-2 text-2xl">
						{item.title}
						<ExplicitMark advisory={item.advisory} />
					</p>
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
					<OptionsMenu
						size="large"
						onlyrics={() => (lyrics = true)}
						onleave={() => (open = false)}
					/>
				</div>
			</div>
		{/if}
	</Sheet.Content>
</Sheet.Root>
