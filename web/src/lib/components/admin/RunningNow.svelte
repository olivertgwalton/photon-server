<script lang="ts">
import { ticking } from "#lib/admin/clock.svelte.js";
import type { Live } from "#lib/admin/live.js";
import { jobKinds, relative, tasks } from "#lib/admin/words.js";
import ScanProgress from "#lib/components/admin/ScanProgress.svelte";

// What the server is doing now, as the admin stream tells it: scans with their
// progress, scheduled tasks and when they started, and running jobs by kind (a
// scan can run hundreds at once).
let { live, libraries }: { live: Live; libraries: Map<string, string> } =
	$props();

const clock = ticking();
const jobs = $derived(
	Object.entries(Object.groupBy(live.jobs, (j) => j.kind)).map(
		([kind, list]) =>
			[kind as keyof typeof jobKinds, list?.length ?? 0] as const,
	),
);
</script>

{#each live.scans as scan (scan.library_id)}
	{@const name = libraries.get(scan.library_id) ?? "a library"}
	<div class="grid gap-1" role="status">
		<p class="text-ink text-sm font-semibold">Scanning {name}</p>
		<ScanProgress {scan} {name} />
	</div>
{/each}
{#if live.tasks.length}
	<ul class="grid gap-1 text-sm" aria-label="Scheduled tasks running">
		{#each live.tasks as task (task.key)}
			<li class="flex justify-between gap-4">
				<span class="text-ink">{tasks[task.key].name}</span>
				<span class="text-ink-3">
					started {relative(task.started_at, clock.now)}
				</span>
			</li>
		{/each}
	</ul>
{/if}
{#if jobs.length}
	<ul class="grid gap-1 text-sm" aria-label="Jobs running">
		{#each jobs as [kind, count] (kind)}
			<li class="flex justify-between gap-4">
				<span class="text-ink">{jobKinds[kind]}</span>
				<span class="text-ink-3">{count} running</span>
			</li>
		{/each}
	</ul>
{/if}
{#if !live.scans.length && !live.tasks.length && !jobs.length}
	<p class="text-ink-3 text-sm">
		{live.ready ? "The server is idle." : "Waiting for live updates…"}
	</p>
{/if}
