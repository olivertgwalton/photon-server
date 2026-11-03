<script lang="ts">
import HeartIcon from "@lucide/svelte/icons/heart";
import PlayIcon from "@lucide/svelte/icons/play";
import ShuffleIcon from "@lucide/svelte/icons/shuffle";
import { setMark } from "#lib/actions.ts";
import type { components } from "#lib/api/schema.d.ts";
import CoverHero from "#lib/components/CoverHero.svelte";
import TrackList from "#lib/components/TrackList.svelte";
import { Button } from "#lib/components/ui/button/index.ts";
import { count, runtime } from "#lib/format.ts";
import { discs } from "#lib/music.ts";
import { trackListen } from "#lib/player/listen.ts";
import { listening, type Order } from "#lib/player/listening.svelte.ts";

type Schemas = components["schemas"];

// An album: its cover and artist, and its tracks to play, in order or
// shuffled, as Plex's album page has them.
let { title: t }: { title: Schemas["TitlePage"] } = $props();

// In the order the album plays them.
const tracks = $derived(discs(t.tracks ?? []).flatMap((d) => d.tracks));
const length = $derived(tracks.reduce((n, c) => n + (c.duration_ms ?? 0), 0));
const favourite = $derived(!!t.state?.favourite_at);

function play(from = 0, order: Order = "in_order") {
	listening.play(tracks.map(trackListen), from, order);
}
</script>

<article class="grid gap-10">
	<CoverHero
		title={t}
		shape="square"
		by={t.artist}
		facts={[
			t.year,
			tracks.length > 0 && count(tracks.length, "track"),
			length > 0 && runtime(length),
		]}
	>
		{#snippet actions()}
			<Button
				size="lg"
				class="rounded-full px-5"
				disabled={!tracks.length}
				onclick={() => play()}
			>
				<PlayIcon class="fill-current" />Play
			</Button>
			<Button
				variant="glass"
				size="lg"
				disabled={!tracks.length}
				onclick={() =>
					play(Math.floor(Math.random() * tracks.length), "shuffled")}
			>
				<ShuffleIcon />Shuffle
			</Button>
			<Button
				variant="glass"
				size="icon-lg"
				aria-pressed={favourite}
				aria-label="Favourite"
				title={favourite ? "Remove from favourites" : "Add to favourites"}
				onclick={() => setMark(t.id, "favourite", !favourite)}
			>
				<HeartIcon class={favourite ? "fill-current" : "text-ink-3"} />
			</Button>
		{/snippet}
	</CoverHero>

	{#if tracks.length}
		<section aria-label="Tracks" class="max-w-4xl">
			<TrackList
				{tracks}
				artist={t.artist?.title}
				onplay={(track) => play(tracks.indexOf(track))}
			/>
		</section>
	{/if}
</article>
