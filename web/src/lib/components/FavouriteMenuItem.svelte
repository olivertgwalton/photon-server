<script lang="ts">
import HeartIcon from "@lucide/svelte/icons/heart";
import * as DropdownMenu from "#lib/components/ui/dropdown-menu/index.ts";
import { isFavourite, setFavourite } from "#lib/favourites.svelte.ts";

let { id, at }: { id: string; at?: string | null } = $props();

const on = $derived(isFavourite(id, at));
</script>

<DropdownMenu.Item onSelect={() => setFavourite(id, !on)}>
	<HeartIcon class={on ? "fill-current" : ""} />
	{on ? "Unfavourite" : "Favourite"}
</DropdownMenu.Item>
