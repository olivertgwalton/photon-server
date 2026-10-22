<script lang="ts">
import CheckIcon from "@lucide/svelte/icons/check";
import EpisodesIcon from "@lucide/svelte/icons/list";
import { client } from "#lib/api/client.js";
import { problemMessage } from "#lib/api/problem.js";
import type { components } from "#lib/api/schema.js";
import { blurStyle } from "#lib/blurhash.js";
import Artwork from "#lib/components/Artwork.svelte";
import { buttonVariants } from "#lib/components/ui/button/index.js";
import * as Popover from "#lib/components/ui/popover/index.js";
import { episodeLabel, timecode } from "#lib/format.js";
import { hidesSpoiler } from "#lib/spoilers.js";

type Schemas = components["schemas"];

// The show's episodes in the order they play, to go to another from the
// player. Read again each time it opens, so what was watched is current.
let {
	open = $bindable(false),
	show,
	current,
	portal,
}: {
	open?: boolean;
	show: Schemas["TitleRef"];
	current: string;
	portal: HTMLElement | undefined;
} = $props();

const progress = (e: Schemas["Card"]) =>
	e.state?.position_ms && e.duration_ms
		? Math.min(e.state.position_ms / e.duration_ms, 1)
		: 0;
</script>

<Popover.Root bind:open>
	<Popover.Trigger
		class={buttonVariants({ variant: "ghost", size: "icon" })}
		aria-label="Episodes"
	>
		<EpisodesIcon class="size-5" />
	</Popover.Trigger>
	<Popover.Content
		portalProps={{ to: portal }}
		side="top"
		align="start"
		role="dialog"
		aria-label="Episodes"
		class="w-80 gap-2 p-2"
	>
		<Popover.Title class="text-ink-2 px-2 pt-1 text-xs font-semibold">
			{show.title}
		</Popover.Title>
		<svelte:boundary>
			{@const { data, error } = await client().GET(
				"/api/v1/titles/{id}/playable",
				{
					params: { path: { id: show.id } },
				},
			)}
			{#if !data}
				<p role="alert" class="text-destructive px-2">
					{problemMessage(error)}
				</p>
			{/if}
			<ol class="grid max-h-80 gap-0.5 overflow-y-auto">
				{#each data?.items ?? [] as e (e.id)}
					{@const picture = e.thumb ?? e.backdrop}
					{@const done = progress(e)}
					<li>
						<a
							href="/play/{e.id}"
							data-sveltekit-replacestate
							aria-current={e.id === current ? "true" : undefined}
							class="hover:bg-ink/8 aria-[current]:bg-ink/12 flex items-center gap-3 rounded-md p-1.5 outline-none focus-visible:ring-2"
							{@attach (a) => {
								if (e.id === current) a.scrollIntoView({ block: "center" });
							}}
						>
							<div
								class="bg-ink/8 relative aspect-video w-20 shrink-0 overflow-hidden rounded"
								style={picture ? blurStyle(e.blurhashes?.[picture]) : undefined}
							>
								{#if picture && !hidesSpoiler(e)}
									<Artwork
										id={picture}
										shape="still"
										sizes="80px"
										class="size-full object-cover"
									/>
								{/if}
								{#if done > 0}
									<div class="absolute inset-x-0 bottom-0 h-1 bg-black/50">
										<div
											class="bg-ink h-full"
											style:width="{done * 100}%"
										></div>
									</div>
								{/if}
							</div>
							<div class="min-w-0 text-sm">
								<p class="text-ink-2 flex items-center gap-1.5 text-xs">
									{episodeLabel(
										e.season_number,
										e.episode_number,
										e.episode_end,
									)}
									{#if e.state?.watched_at && !done}
										<CheckIcon class="size-3.5" aria-label="Watched" />
									{/if}
								</p>
								<p class="text-ink line-clamp-1 font-medium">{e.title}</p>
								{#if e.duration_ms}
									<p class="text-ink-2 text-xs">
										{timecode(e.duration_ms)}
									</p>
								{/if}
							</div>
						</a>
					</li>
				{/each}
			</ol>
			{#snippet pending()}
				<p class="text-ink-3 px-2 pb-1">Loading episodes…</p>
			{/snippet}
		</svelte:boundary>
	</Popover.Content>
</Popover.Root>
