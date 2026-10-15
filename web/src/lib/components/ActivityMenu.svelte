<script lang="ts">
import ActivityIcon from "@lucide/svelte/icons/activity";
import { Clock } from "#lib/admin/clock.js";
import { positionAt } from "#lib/admin/live.js";
import { liveStream } from "#lib/admin/stream.svelte.js";
import { byName } from "#lib/admin/words.js";
import type { components } from "#lib/api/schema.js";
import RunningNow from "#lib/components/admin/RunningNow.svelte";
import * as Popover from "#lib/components/ui/popover/index.js";
import { Progress } from "#lib/components/ui/progress/index.js";
import { fullTitle } from "#lib/format.js";

// What the server is doing, beside the profile, as Plex's activity panel is:
// drawn only while something plays or runs.
let { libraries }: { libraries: components["schemas"]["Library"][] } = $props();

// A task over within moments (the stalled-job sweep runs every minute) is not
// worth the icon appearing for.
const brief = 2_000;

const live = liveStream();
const clock = new Clock();
const names = $derived(byName(libraries));
const state = $derived({
	...live.state,
	tasks: live.state.tasks.filter(
		(t) => clock.now - Date.parse(t.started_at) >= brief,
	),
});
const working = $derived(
	state.scans.length +
		state.tasks.length +
		new Set(state.jobs.map((j) => j.kind)).size,
);
const running = $derived(working + state.playbacks.length);
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
			class="grid w-80 grid-cols-1 gap-3"
			role="dialog"
			aria-label="Activity"
		>
			<Popover.Title class="text-ink font-semibold">Activity</Popover.Title>
			{#if state.playbacks.length}
				<ul class="grid grid-cols-1 gap-3 text-sm" aria-label="Streams">
					{#each state.playbacks as p (p.id)}
						{@const name = fullTitle(p.title, p.title.show)}
						<li class="grid grid-cols-1 gap-1.5">
							<p class="flex justify-between gap-4">
								<a
									href="/titles/{p.title.id}"
									class="text-ink truncate hover:underline"
									>{name}</a
								>
								<span class="text-ink-3 shrink-0">
									{p.state === "paused" ? "Paused" : ""}
								</span>
							</p>
							<p class="text-ink-3 truncate">
								{p.profile.name}
								· {p.device.name}
							</p>
							<!-- Moved on each second, unanimated, as on the dashboard. -->
							<Progress
								class="*:transition-none"
								value={positionAt(p, clock.now)}
								max={Math.max(p.version.duration_ms, 1)}
								aria-label="How far {p.profile.name} is through {name}"
							/>
						</li>
					{/each}
				</ul>
			{/if}
			{#if working}
				<RunningNow live={state} libraries={names} />
			{/if}
		</Popover.Content>
	</Popover.Root>
{/if}
