<script lang="ts">
import ActivityIcon from "@lucide/svelte/icons/activity";
import type { components } from "#lib/api/schema.js";
import { ticking } from "#lib/admin/clock.svelte.js";
import { liveStream } from "#lib/admin/stream.svelte.js";
import RunningNow from "#lib/components/admin/RunningNow.svelte";
import * as Popover from "#lib/components/ui/popover/index.js";

// What the server is doing, beside the profile, as Plex's activity panel is:
// drawn only while something runs.
let { libraries }: { libraries: components["schemas"]["Library"][] } = $props();

// A task over within moments (the stalled-job sweep runs every minute) is not
// worth the icon appearing for.
const brief = 2_000;

const live = liveStream();
const clock = ticking();
const names = $derived(new Map(libraries.map((l) => [l.id, l.name] as const)));
const state = $derived({
	...live.state,
	tasks: live.state.tasks.filter(
		(t) => clock.now - Date.parse(t.started_at) >= brief,
	),
});
const running = $derived(
	state.scans.length + state.tasks.length + state.jobs.length,
);
</script>

{#if running}
	<Popover.Root>
		<Popover.Trigger
			class="hover:bg-raise text-ink grid size-9 place-items-center rounded-full outline-none"
			aria-label="Activity: {running} running"
		>
			<ActivityIcon class="size-5 motion-safe:animate-pulse" />
		</Popover.Trigger>
		<Popover.Content
			align="end"
			class="grid w-80 gap-3"
			role="dialog"
			aria-label="Activity"
		>
			<Popover.Title class="text-ink font-semibold">Activity</Popover.Title>
			<RunningNow live={state} libraries={names} />
		</Popover.Content>
	</Popover.Root>
{/if}
