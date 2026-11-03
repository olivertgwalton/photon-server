<script lang="ts">
import GaugeIcon from "@lucide/svelte/icons/gauge";
import * as DropdownMenu from "#lib/components/ui/dropdown-menu/index.ts";
import { listening } from "#lib/player/listening.svelte.ts";

// How fast what plays is played, as a menu's submenu: an audiobook is
// listened to faster or slower, as Audiobookshelf's is.
const rates = ["0.75", "1", "1.25", "1.5", "1.75", "2"];
</script>

<DropdownMenu.Sub>
	<DropdownMenu.SubTrigger>
		<GaugeIcon />Playback speed
		<span class="text-ink-3 ml-auto text-xs">{listening.rate}×</span>
	</DropdownMenu.SubTrigger>
	<DropdownMenu.SubContent>
		<DropdownMenu.RadioGroup
			value={String(listening.rate)}
			onValueChange={(v) => listening.setRate(Number(v))}
		>
			{#each rates as rate (rate)}
				<DropdownMenu.RadioItem value={rate}>{rate}×</DropdownMenu.RadioItem>
			{/each}
		</DropdownMenu.RadioGroup>
	</DropdownMenu.SubContent>
</DropdownMenu.Sub>
