<script lang="ts">
import AudioWaveformIcon from "@lucide/svelte/icons/audio-waveform";
import Settings2Icon from "@lucide/svelte/icons/settings-2";
import Volume1Icon from "@lucide/svelte/icons/volume-1";
import IconButton from "#lib/components/IconButton.svelte";
import * as DropdownMenu from "#lib/components/ui/dropdown-menu/index.ts";
import {
	type GainMode,
	gainModes,
	type Quality,
	qualities,
} from "#lib/player/listen.ts";
import { listening } from "#lib/player/listening.svelte.ts";
import SleepMenu from "./SleepMenu.svelte";
import SpeedMenu from "./SpeedMenu.svelte";

// How the player sends and plays: the quality asked of the server, its
// loudness, the speed, and when it stops by itself.
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
		<SpeedMenu />
		<SleepMenu />
	</DropdownMenu.Content>
</DropdownMenu.Root>
