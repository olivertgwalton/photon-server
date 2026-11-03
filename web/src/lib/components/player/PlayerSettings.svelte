<script lang="ts">
import AudioWaveformIcon from "@lucide/svelte/icons/audio-waveform";
import GaugeIcon from "@lucide/svelte/icons/gauge";
import MoonIcon from "@lucide/svelte/icons/moon";
import Settings2Icon from "@lucide/svelte/icons/settings-2";
import Volume1Icon from "@lucide/svelte/icons/volume-1";
import IconButton from "#lib/components/IconButton.svelte";
import * as DropdownMenu from "#lib/components/ui/dropdown-menu/index.ts";
import { timecode } from "#lib/format.ts";
import {
	type GainMode,
	gainModes,
	type Quality,
	qualities,
} from "#lib/player/listen.ts";
import { listening } from "#lib/player/listening.svelte.ts";

// How the player sends and plays: the quality asked of the server, the speed,
// and when it stops by itself.
const qualityNames: Record<Quality, string> = {
	original: "Original",
	"320": "320 kbps",
	"192": "192 kbps",
	"128": "128 kbps",
};
const gainNames: Record<GainMode, string> = {
	track: "By track",
	album: "By album",
	off: "Off",
};
const rates = ["0.75", "1", "1.25", "1.5", "1.75", "2"];
const sleeps = [15, 30, 45, 60];

// What is left of a timed sleep, said again each second while the menu is
// open.
let now = $state(Date.now());
$effect(() => {
	if (listening.sleep.kind !== "at") return;
	const timer = setInterval(() => (now = Date.now()), 1000);
	return () => clearInterval(timer);
});
const sleepValue = $derived(
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

function chooseSleep(value: string) {
	if (value === "off") listening.setSleep({ kind: "off" });
	else if (value === "end") listening.sleepAtEnd();
	else
		listening.setSleep({
			kind: "at",
			until: Date.now() + Number(value) * 60_000,
		});
}
</script>

<DropdownMenu.Root>
	<DropdownMenu.Trigger>
		{#snippet child({ props })}
			<IconButton {...props} label="Player settings" icon={Settings2Icon} />
		{/snippet}
	</DropdownMenu.Trigger>
	<DropdownMenu.Content align="end" class="w-60">
		<DropdownMenu.Sub>
			<DropdownMenu.SubTrigger>
				<AudioWaveformIcon />Quality
				<span class="text-ink-3 ml-auto text-xs">
					{qualityNames[listening.quality]}
				</span>
			</DropdownMenu.SubTrigger>
			<DropdownMenu.SubContent>
				<DropdownMenu.RadioGroup
					value={listening.quality}
					onValueChange={(v) => {
						const q = qualities.find((q) => q === v);
						if (q) listening.setQuality(q);
					}}
				>
					{#each qualities as q (q)}
						<DropdownMenu.RadioItem value={q}>
							{qualityNames[q]}
						</DropdownMenu.RadioItem>
					{/each}
				</DropdownMenu.RadioGroup>
			</DropdownMenu.SubContent>
		</DropdownMenu.Sub>
		<DropdownMenu.Sub>
			<DropdownMenu.SubTrigger>
				<Volume1Icon />Volume levelling
				<span class="text-ink-3 ml-auto text-xs">
					{gainNames[listening.gainMode]}
				</span>
			</DropdownMenu.SubTrigger>
			<DropdownMenu.SubContent>
				<DropdownMenu.RadioGroup
					value={listening.gainMode}
					onValueChange={(v) => {
						const mode = gainModes.find((m) => m === v);
						if (mode) listening.setGainMode(mode);
					}}
				>
					{#each gainModes as mode (mode)}
						<DropdownMenu.RadioItem value={mode}>
							{gainNames[mode]}
						</DropdownMenu.RadioItem>
					{/each}
				</DropdownMenu.RadioGroup>
			</DropdownMenu.SubContent>
		</DropdownMenu.Sub>
		<DropdownMenu.Sub>
			<DropdownMenu.SubTrigger>
				<GaugeIcon />Speed
				<span class="text-ink-3 ml-auto text-xs">{listening.rate}×</span>
			</DropdownMenu.SubTrigger>
			<DropdownMenu.SubContent>
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
			</DropdownMenu.SubContent>
		</DropdownMenu.Sub>
		<DropdownMenu.Sub>
			<DropdownMenu.SubTrigger>
				<MoonIcon />Sleep timer
				<span class="text-ink-3 ml-auto font-mono text-xs tabular-nums">
					{asleepIn ?? (sleepValue === "end" ? "At the end" : "Off")}
				</span>
			</DropdownMenu.SubTrigger>
			<DropdownMenu.SubContent>
				<DropdownMenu.RadioGroup value={sleepValue} onValueChange={chooseSleep}>
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
	</DropdownMenu.Content>
</DropdownMenu.Root>
