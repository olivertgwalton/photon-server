<script lang="ts">
import ChevronUpIcon from "@lucide/svelte/icons/chevron-up";
import ListMusicIcon from "@lucide/svelte/icons/list-music";
import Volume2Icon from "@lucide/svelte/icons/volume-2";
import VolumeXIcon from "@lucide/svelte/icons/volume-x";
import XIcon from "@lucide/svelte/icons/x";
import Artwork from "#lib/components/Artwork.svelte";
import ExplicitMark from "#lib/components/ExplicitMark.svelte";
import IconButton from "#lib/components/IconButton.svelte";
import { Slider } from "#lib/components/ui/slider/index.ts";
import { timecode } from "#lib/format.ts";
import { chapterAt, keyAction } from "#lib/player/listen.ts";
import {
	listening,
	mediaSession,
	showNowPlaying,
} from "#lib/player/listening.svelte.ts";
import NowPlaying from "./NowPlaying.svelte";
import OptionsMenu from "./OptionsMenu.svelte";
import PlayerControls from "./PlayerControls.svelte";
import PlayerSettings from "./PlayerSettings.svelte";
import QueuePanel from "./QueuePanel.svelte";
import SeekBar from "./SeekBar.svelte";

// The audio player's bar, along the foot of the shell while anything is
// queued: what plays, its controls and its queue, as Plex's and Jellyfin's
// web players have it. Its cover opens the Now Playing view.
const item = $derived(listening.current);
const chapter = $derived(chapterAt(listening.position, item?.chapters));

let queueOpen = $state(false);
let nowPlayingOpen = $state(false);
let lyricsShown = $state(false);

$effect(() => listening.start());
$effect(() => mediaSession());
$effect(() => showNowPlaying(item));
$effect(() => {
	if (!("mediaSession" in navigator)) return;
	navigator.mediaSession.playbackState = listening.playing
		? "playing"
		: "paused";
	if (listening.duration > 0) {
		navigator.mediaSession.setPositionState({
			duration: listening.duration,
			position: Math.min(listening.position, listening.duration),
			playbackRate: listening.rate,
		});
	}
});

// The player's keys, wherever in the shell, but never while typing, and
// Space never on a control it would press.
function onkeydown(e: KeyboardEvent) {
	if (!item || e.defaultPrevented) return;
	const target = e.target;
	if (
		target instanceof HTMLElement &&
		(target.isContentEditable ||
			target.closest("input, textarea, select, [role=menu], [role=listbox]") ||
			(e.key === " " && target.closest("button, a, [role=button]")))
	)
		return;
	const action = keyAction(e);
	if (!action) return;
	e.preventDefault();
	switch (action) {
		case "toggle":
			return listening.toggle();
		case "back":
			return listening.seek(listening.position - 10);
		case "forward":
			return listening.seek(listening.position + 10);
		case "next":
			return listening.next();
		case "previous":
			return listening.previous();
		case "mute":
			return listening.toggleMute();
	}
}
</script>

<svelte:window {onkeydown} />

{#if item}
	<section
		aria-label="Now playing"
		class="border-line bg-ground/90 sticky bottom-0 z-20 grid grid-cols-[minmax(0,1fr)_auto] items-center gap-x-4 border-t px-3 py-2 backdrop-blur-xl sm:px-4 md:grid-cols-[minmax(12rem,1fr)_minmax(0,32rem)_minmax(0,1fr)]"
	>
		<div class="flex min-w-0 items-center gap-3">
			<button
				type="button"
				class="group bg-raise relative size-12 shrink-0 overflow-hidden rounded-md outline-none focus-visible:ring-2"
				aria-label="Open Now Playing"
				onclick={() => (nowPlayingOpen = true)}
			>
				{#if item.artwork}
					{#key item.artwork}
						<Artwork
							id={item.artwork}
							shape="square"
							sizes="3rem"
							class="size-full object-cover"
						/>
					{/key}
				{/if}
				<span
					class="absolute inset-0 grid place-items-center bg-black/50 opacity-0 transition-opacity group-hover:opacity-100 group-focus-visible:opacity-100"
				>
					<ChevronUpIcon class="size-5" aria-hidden="true" />
				</span>
			</button>
			<div class="min-w-0">
				<p
					class="text-ink flex min-w-0 items-center gap-1.5 text-sm font-medium"
				>
					<a
						href="/titles/{item.from?.id ?? item.id}"
						class="truncate hover:underline"
					>
						{item.title}
					</a>
					<ExplicitMark advisory={item.advisory} />
				</p>
				<p class="text-ink-3 truncate text-xs">
					{chapter?.title ?? item.by ?? ""}
				</p>
			</div>
		</div>

		<div class="grid justify-items-center gap-1 max-md:justify-items-end">
			<PlayerControls size="bar" />
			<div
				class="text-ink-3 flex w-full items-center gap-2 font-mono text-xs tabular-nums max-md:hidden"
			>
				<span>{timecode(listening.position * 1000)}</span>
				<div class="min-w-0 flex-1">
					<SeekBar
						position={listening.position}
						duration={listening.duration}
						buffered={listening.buffered}
						chapters={item.chapters ?? []}
						onseek={listening.seek}
					/>
				</div>
				<span>{timecode(listening.duration * 1000)}</span>
			</div>
		</div>

		<div class="flex items-center justify-end gap-1 max-md:hidden">
			<OptionsMenu
				size="bar"
				onlyrics={() => {
					lyricsShown = true;
					nowPlayingOpen = true;
				}}
			/>
			<PlayerSettings />
			<IconButton
				label="Queue"
				icon={ListMusicIcon}
				aria-pressed={queueOpen}
				onclick={() => (queueOpen = !queueOpen)}
			/>
			<IconButton
				label={listening.muted ? "Unmute" : "Mute"}
				icon={listening.muted || listening.volume === 0
					? VolumeXIcon
					: Volume2Icon}
				onclick={listening.toggleMute}
			/>
			<Slider
				type="single"
				min={0}
				max={1}
				step={0.01}
				value={listening.volume}
				onValueChange={listening.setVolume}
				aria-label="Volume"
				class="w-24 max-xl:hidden"
			/>
			<IconButton label="Close" icon={XIcon} onclick={listening.close} />
		</div>
	</section>
	<QueuePanel bind:open={queueOpen} />
	<NowPlaying
		bind:open={nowPlayingOpen}
		bind:lyrics={lyricsShown}
		onqueue={() => {
			nowPlayingOpen = false;
			queueOpen = true;
		}}
	/>
{/if}
