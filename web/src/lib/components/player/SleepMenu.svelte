<script lang="ts">
import MoonIcon from "@lucide/svelte/icons/moon";
import * as DropdownMenu from "#lib/components/ui/dropdown-menu/index.ts";
import { timecode } from "#lib/format.ts";
import { listening } from "#lib/player/listening.svelte.ts";

// When the player stops by itself, as a menu's submenu, with what is left of
// a timed sleep said again each second while it is set.
const sleeps = [15, 30, 45, 60];

let now = $state(Date.now());
$effect(() => {
	if (listening.sleep.kind !== "at") return;
	const timer = setInterval(() => (now = Date.now()), 1000);
	return () => clearInterval(timer);
});
const value = $derived(
	listening.sleep.kind === "off"
		? "off"
		: listening.sleep.kind === "end"
			? "end"
			: "timed",
);
const asleepIn = $derived(
	listening.sleep.kind === "at"
		? timecode(Math.max(0, listening.sleep.until - now))
		: undefined,
);

function choose(v: string) {
	if (v === "off") listening.setSleep({ kind: "off" });
	else if (v === "end") listening.sleepAtEnd();
	else
		listening.setSleep({ kind: "at", until: Date.now() + Number(v) * 60_000 });
}
</script>

<DropdownMenu.Sub>
	<DropdownMenu.SubTrigger>
		<MoonIcon />Sleep timer
		<span class="text-ink-3 ml-auto font-mono text-xs tabular-nums">
			{asleepIn ?? (value === "end" ? "At the end" : "Off")}
		</span>
	</DropdownMenu.SubTrigger>
	<DropdownMenu.SubContent>
		<DropdownMenu.RadioGroup {value} onValueChange={choose}>
			<DropdownMenu.RadioItem value="off">Off</DropdownMenu.RadioItem>
			{#each sleeps as minutes (minutes)}
				<DropdownMenu.RadioItem value={String(minutes)}>
					{minutes}
					minutes
				</DropdownMenu.RadioItem>
			{/each}
			<DropdownMenu.RadioItem value="end">
				{listening.current?.chapters?.length
					? "End of chapter"
					: "End of track"}
			</DropdownMenu.RadioItem>
		</DropdownMenu.RadioGroup>
	</DropdownMenu.SubContent>
</DropdownMenu.Sub>
