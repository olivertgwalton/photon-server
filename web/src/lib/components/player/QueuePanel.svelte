<script lang="ts">
import AudioLinesIcon from "@lucide/svelte/icons/audio-lines";
import GripVerticalIcon from "@lucide/svelte/icons/grip-vertical";
import ListPlusIcon from "@lucide/svelte/icons/list-plus";
import ListXIcon from "@lucide/svelte/icons/list-x";
import XIcon from "@lucide/svelte/icons/x";
import Artwork from "#lib/components/Artwork.svelte";
import { Button } from "#lib/components/ui/button/index.ts";
import { ScrollArea } from "#lib/components/ui/scroll-area/index.ts";
import * as Sheet from "#lib/components/ui/sheet/index.ts";
import * as Tabs from "#lib/components/ui/tabs/index.ts";
import { timecode } from "#lib/format.ts";
import { chapterAt, type Queued } from "#lib/player/listen.ts";
import { listening } from "#lib/player/listening.svelte.ts";
import { reorder } from "#lib/reorder.svelte.ts";
import { getShell } from "#lib/shell.svelte.ts";

// The queue, as Plexamp's and Finamp's: what played, what plays, and what is
// next, put in order by dragging; an audiobook's chapters beside it.
let { open = $bindable(false) }: { open: boolean } = $props();

const shell = getShell();
const chapters = $derived(listening.current?.chapters ?? []);
const chapter = $derived(chapterAt(listening.position, chapters));
const played = $derived(listening.queue.slice(0, listening.at));
const upcoming = $derived(listening.queue.slice(listening.at + 1));
const drag = reorder((from, to) => listening.move(from, to));
let tab = $state<"queue" | "chapters">("queue");

function save() {
	open = false;
	shell.pickPlaylist(
		listening.queue.map((q) => q.id),
		"The queue",
	);
}
</script>

{#snippet row(q: Queued, index: number, tone: "played" | "playing" | "next")}
	<li
		{@attach drag.row(index)}
		class={[
			"group hover:bg-raise flex items-center gap-2 rounded-md py-1.5 pr-1 pl-1",
			drag.dragging === index && "opacity-50",
			tone === "played" && "opacity-60",
		]}
	>
		<GripVerticalIcon
			class="text-ink-3 size-4 shrink-0 cursor-grab opacity-0 group-hover:opacity-100"
			aria-hidden="true"
		/>
		<button
			type="button"
			class="flex min-w-0 flex-1 items-center gap-3 text-left outline-none focus-visible:underline"
			onclick={() => listening.jump(index)}
		>
			<span class="bg-raise relative size-10 shrink-0 overflow-hidden rounded">
				{#if q.artwork}
					<Artwork
						id={q.artwork}
						shape="square"
						sizes="2.5rem"
						class="size-full object-cover"
					/>
				{/if}
				{#if tone === "playing"}
					<span class="absolute inset-0 grid place-items-center bg-black/50">
						<AudioLinesIcon class="text-signal size-4" aria-label="Playing" />
					</span>
				{/if}
			</span>
			<span class="min-w-0">
				<span
					class={[
						"block truncate text-sm",
						tone === "playing" ? "text-signal" : "text-ink",
					]}
				>
					{q.title}
				</span>
				{#if q.by}
					<span class="text-ink-3 block truncate text-xs">{q.by}</span>
				{/if}
			</span>
		</button>
		<Button
			variant="ghost"
			size="icon-sm"
			aria-label="Remove {q.title}"
			class="text-ink-3 opacity-0 group-hover:opacity-100 focus-visible:opacity-100"
			onclick={() => listening.remove(index)}
		>
			<XIcon />
		</Button>
	</li>
{/snippet}

<Sheet.Root bind:open>
	<Sheet.Content side="right" class="w-full gap-0 sm:max-w-md">
		<Sheet.Header>
			<Sheet.Title>{tab === "chapters" ? "Chapters" : "Queue"}</Sheet.Title>
		</Sheet.Header>
		<Tabs.Root
			value={tab}
			onValueChange={(v) => (tab = v === "chapters" ? "chapters" : "queue")}
			class="min-h-0 flex-1 px-4 pb-4"
		>
			{#if chapters.length}
				<Tabs.List class="mb-2 w-full">
					<Tabs.Trigger value="queue">Queue</Tabs.Trigger>
					<Tabs.Trigger value="chapters">Chapters</Tabs.Trigger>
				</Tabs.List>
			{/if}
			<Tabs.Content value="queue" class="flex min-h-0 flex-col gap-3">
				<div class="flex gap-2">
					<Button
						variant="outline"
						size="sm"
						disabled={!listening.queue.length}
						onclick={save}
					>
						<ListPlusIcon />Save as playlist
					</Button>
					<Button
						variant="ghost"
						size="sm"
						disabled={!upcoming.length}
						onclick={listening.clearUpcoming}
					>
						<ListXIcon />Clear up next
					</Button>
				</div>
				<ScrollArea class="min-h-0 flex-1">
					{#if played.length}
						<h3 class="text-ink-3 mt-2 mb-1 text-xs font-semibold uppercase">
							Played
						</h3>
						<ol>
							{#each played as q, i (q.key)}
								{@render row(q, i, "played")}
							{/each}
						</ol>
					{/if}
					{#if listening.current}
						<h3 class="text-ink-3 mt-3 mb-1 text-xs font-semibold uppercase">
							Now playing
						</h3>
						<ol>
							{@render row(listening.current, listening.at, "playing")}
						</ol>
					{/if}
					<h3 class="text-ink-3 mt-3 mb-1 text-xs font-semibold uppercase">
						Up next
					</h3>
					{#if upcoming.length}
						<ol>
							{#each upcoming as q, i (q.key)}
								{@render row(q, listening.at + 1 + i, "next")}
							{/each}
						</ol>
					{:else}
						<p class="text-ink-3 px-2 py-3 text-sm">
							Nothing after this. Choose Play next or Add to queue in any
							track's, album's or artist's menu.
						</p>
					{/if}
				</ScrollArea>
			</Tabs.Content>
			<Tabs.Content value="chapters" class="min-h-0">
				<ScrollArea class="h-full">
					<ol>
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
					</ol>
				</ScrollArea>
			</Tabs.Content>
		</Tabs.Root>
	</Sheet.Content>
</Sheet.Root>
