<script lang="ts">
import ListEndIcon from "@lucide/svelte/icons/list-end";
import ListPlusIcon from "@lucide/svelte/icons/list-plus";
import ListStartIcon from "@lucide/svelte/icons/list-start";
import { toast } from "svelte-sonner";
import * as DropdownMenu from "#lib/components/ui/dropdown-menu/index.ts";
import type { Listen } from "#lib/player/listen.ts";
import { listening } from "#lib/player/listening.svelte.ts";
import { getShell } from "#lib/shell.svelte.ts";

// What a track's, an album's or an artist's menu does with the queue, as
// Plexamp's and Finamp's menus have it, and the playlist it may go in.
let {
	ids,
	name,
	tracks,
	playlist = "shown",
}: {
	ids: string[];
	name: string;
	tracks: () => Promise<Listen[]>;
	// A menu that has its own Add to playlist leaves this one's out.
	playlist?: "shown" | "hidden";
} = $props();

const shell = getShell();

async function queue(where: "next" | "last") {
	const items = await tracks();
	if (!items.length) return;
	if (where === "next") listening.playNext(items);
	else listening.addToQueue(items);
	toast.success(
		where === "next" ? `${name} plays next.` : `${name} is queued.`,
	);
}
</script>

<DropdownMenu.Item onSelect={() => queue("next")}>
	<ListStartIcon />Play next
</DropdownMenu.Item>
<DropdownMenu.Item onSelect={() => queue("last")}>
	<ListEndIcon />Add to queue
</DropdownMenu.Item>
{#if playlist === "shown"}
	<DropdownMenu.Item onSelect={() => shell.pickPlaylist(ids, name)}>
		<ListPlusIcon />Add to playlist…
	</DropdownMenu.Item>
{/if}
