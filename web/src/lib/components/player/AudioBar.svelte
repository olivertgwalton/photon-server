<script lang="ts">
import GaugeIcon from "@lucide/svelte/icons/gauge";
import ListMusicIcon from "@lucide/svelte/icons/list-music";
import PauseIcon from "@lucide/svelte/icons/pause";
import PlayIcon from "@lucide/svelte/icons/play";
import SkipBackIcon from "@lucide/svelte/icons/skip-back";
import SkipForwardIcon from "@lucide/svelte/icons/skip-forward";
import Volume2Icon from "@lucide/svelte/icons/volume-2";
import VolumeXIcon from "@lucide/svelte/icons/volume-x";
import XIcon from "@lucide/svelte/icons/x";
import Artwork from "#lib/components/Artwork.svelte";
import IconButton from "#lib/components/IconButton.svelte";
import { Button } from "#lib/components/ui/button/index.ts";
import * as DropdownMenu from "#lib/components/ui/dropdown-menu/index.ts";
import * as Popover from "#lib/components/ui/popover/index.ts";
import { ScrollArea } from "#lib/components/ui/scroll-area/index.ts";
import { Slider } from "#lib/components/ui/slider/index.ts";
import { timecode } from "#lib/format.ts";
import { chapterAt } from "#lib/player/listen.ts";
import {
	listening,
	mediaSession,
	showNowPlaying,
} from "#lib/player/listening.svelte.ts";
import SeekBar from "./SeekBar.svelte";

// The audio player's bar, along the foot of the shell while anything is
// queued: what plays, its controls and its queue, as Plex's and Jellyfin's
// web players have it.
const item = $derived(listening.current);
const chapters = $derived(item?.chapters ?? []);
const chapter = $derived(chapterAt(listening.position, chapters));

// An audiobook is listened to faster or slower, as Audiobookshelf's is.
const rates = ["0.75", "1", "1.25", "1.5", "1.75", "2"];

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
</script>

{#if item}
	<section
		aria-label="Now playing"
		class="border-line bg-ground/90 sticky bottom-0 z-20 grid grid-cols-[minmax(0,1fr)_auto] items-center gap-x-4 border-t px-3 py-2 backdrop-blur-xl sm:px-4 md:grid-cols-[minmax(12rem,1fr)_minmax(0,32rem)_minmax(0,1fr)]"
	>
		<div class="flex min-w-0 items-center gap-3">
			<div class="bg-raise size-12 shrink-0 overflow-hidden rounded-md">
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
			</div>
			<div class="min-w-0">
				<p class="text-ink truncate text-sm font-medium">
					{#if item.from}
						<a href="/titles/{item.from.id}" class="hover:underline">
							{item.title}
						</a>
					{:else}
						<a href="/titles/{item.id}" class="hover:underline">
							{item.title}
						</a>
					{/if}
				</p>
				<p class="text-ink-3 truncate text-xs">
					{chapter?.title ?? item.by ?? ""}
				</p>
			</div>
		</div>

		<div class="grid justify-items-center gap-1 max-md:justify-items-end">
			<div class="flex items-center gap-1">
				<Button
					variant="ghost"
					size="icon"
					aria-label="Previous"
					class="max-sm:hidden"
					onclick={listening.previous}
				>
					<SkipBackIcon class="fill-current" />
				</Button>
				<Button
					size="icon"
					class="rounded-full"
					aria-label={listening.playing ? "Pause" : "Play"}
					onclick={listening.toggle}
				>
					{#if listening.playing}
						<PauseIcon class="fill-current" />
					{:else}
						<PlayIcon class="fill-current" />
					{/if}
				</Button>
				<Button
					variant="ghost"
					size="icon"
					aria-label="Next"
					onclick={listening.next}
				>
					<SkipForwardIcon class="fill-current" />
				</Button>
			</div>
			<div
				class="text-ink-3 flex w-full items-center gap-2 font-mono text-xs tabular-nums max-md:hidden"
			>
				<span>{timecode(listening.position * 1000)}</span>
				<div class="min-w-0 flex-1">
					<SeekBar
						position={listening.position}
						duration={listening.duration}
						buffered={listening.buffered}
						{chapters}
						onseek={listening.seek}
					/>
				</div>
				<span>{timecode(listening.duration * 1000)}</span>
			</div>
		</div>

		<div class="flex items-center justify-end gap-1 max-md:hidden">
			{#if chapters.length}
				<DropdownMenu.Root>
					<DropdownMenu.Trigger>
						{#snippet child({ props })}
							<Button
								{...props}
								variant="ghost"
								size="sm"
								class="text-ink-2 font-mono tabular-nums"
								aria-label="Speed"
							>
								<GaugeIcon />{listening.rate}×
							</Button>
						{/snippet}
					</DropdownMenu.Trigger>
					<DropdownMenu.Content align="end">
						<DropdownMenu.RadioGroup
							value={String(listening.rate)}
							onValueChange={(v) => listening.setRate(Number(v))}
						>
							{#each rates as rate (rate)}
								<DropdownMenu.RadioItem value={rate}
									>{rate}×</DropdownMenu.RadioItem
								>
							{/each}
						</DropdownMenu.RadioGroup>
					</DropdownMenu.Content>
				</DropdownMenu.Root>
			{/if}
			<Popover.Root>
				<Popover.Trigger>
					{#snippet child({ props })}
						<IconButton
							{...props}
							label={chapters.length ? "Chapters" : "Queue"}
							icon={ListMusicIcon}
						/>
					{/snippet}
				</Popover.Trigger>
				<Popover.Content align="end" class="w-80 p-0">
					<ScrollArea class="max-h-96">
						<ol class="grid p-1">
							{#if chapters.length}
								{#each chapters as c, i (c.start_ms)}
									<li>
										<button
											type="button"
											aria-current={c === chapter ? "true" : undefined}
											class="hover:bg-raise aria-[current=true]:text-signal flex w-full items-baseline gap-3 rounded-md px-3 py-2 text-left text-sm"
											onclick={() => listening.seek(c.start_ms / 1000)}
										>
											<span class="min-w-0 flex-1 truncate">
												{c.title || `Chapter ${i + 1}`}
											</span>
											<span class="text-ink-3 font-mono text-xs tabular-nums">
												{timecode(c.start_ms)}
											</span>
										</button>
									</li>
								{/each}
							{:else}
								{#each listening.queue as q, i (`${i}:${q.id}`)}
									<li>
										<button
											type="button"
											aria-current={i === listening.at ? "true" : undefined}
											class="hover:bg-raise aria-[current=true]:text-signal flex w-full flex-col rounded-md px-3 py-2 text-left"
											onclick={() => listening.jump(i)}
										>
											<span class="truncate text-sm">{q.title}</span>
											{#if q.by}
												<span class="text-ink-3 truncate text-xs">{q.by}</span>
											{/if}
										</button>
									</li>
								{/each}
							{/if}
						</ol>
					</ScrollArea>
				</Popover.Content>
			</Popover.Root>
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
{/if}
