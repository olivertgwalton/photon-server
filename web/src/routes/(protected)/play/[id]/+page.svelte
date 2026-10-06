<script lang="ts">
import { page } from "$app/state";
import Player from "#lib/components/player/Player.svelte";

let { data } = $props();

// ?t= is where to start, in seconds; without it the reader resumes where they
// left off. ?version=, ?audio= and ?subtitle= choose the copy and its tracks,
// the tracks by stream index.
const number = (name: string) => {
	const value = page.url.searchParams.get(name);
	return value === null || value === "" || Number.isNaN(Number(value))
		? undefined
		: Number(value);
};
</script>

{#key data.title.id}
	<Player
		title={data.title}
		start={number("t") ?? (data.title.state?.position_ms ?? 0) / 1000}
		version={page.url.searchParams.get("version") ?? undefined}
		audio={number("audio")}
		subtitle={number("subtitle")}
	/>
{/key}
