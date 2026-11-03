<script lang="ts">
import HeartIcon from "@lucide/svelte/icons/heart";
import PlayIcon from "@lucide/svelte/icons/play";
import ShuffleIcon from "@lucide/svelte/icons/shuffle";
import { toast } from "svelte-sonner";
import { setMark } from "#lib/actions.ts";
import { client } from "#lib/api/client.ts";
import { problemMessage } from "#lib/api/problem.ts";
import type { components } from "#lib/api/schema.d.ts";
import CardGrid from "#lib/components/CardGrid.svelte";
import CoverHero from "#lib/components/CoverHero.svelte";
import { Button } from "#lib/components/ui/button/index.ts";
import { count } from "#lib/format.ts";
import { trackListen } from "#lib/player/listen.ts";
import { listening, type Order } from "#lib/player/listening.svelte.ts";

type Schemas = components["schemas"];

// An artist: their picture, and their albums, the newest first, as Plex's
// artist page has them; every track of theirs plays from here.
let { title: t }: { title: Schemas["TitlePage"] } = $props();

const albums = $derived(
	(t.albums ?? []).toSorted((a, b) => (b.year ?? 0) - (a.year ?? 0)),
);
const favourite = $derived(!!t.state?.favourite_at);

async function play(order: Order) {
	const { data, error } = await client().GET("/api/v1/titles/{id}/playable", {
		params: { path: { id: t.id } },
	});
	if (error) toast.error(problemMessage(error));
	else
		listening.play(
			data.items.map(trackListen),
			order === "shuffled" ? Math.floor(Math.random() * data.items.length) : 0,
			order,
		);
}
</script>

<article class="grid gap-10">
	<CoverHero
		title={t}
		shape="square"
		facts={[albums.length > 0 && count(albums.length, "album")]}
	>
		{#snippet actions()}
			<Button
				size="lg"
				class="rounded-full px-5"
				disabled={!albums.length}
				onclick={() => play("in_order")}
			>
				<PlayIcon class="fill-current" />Play
			</Button>
			<Button
				variant="glass"
				size="lg"
				disabled={!albums.length}
				onclick={() => play("shuffled")}
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

	{#if albums.length}
		<section aria-labelledby="albums">
			<h2 id="albums" class="heading mb-3">Albums</h2>
			<CardGrid cards={albums} shape="square" />
		</section>
	{/if}
</article>
