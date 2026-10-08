<script lang="ts">
import { ticking } from "#lib/admin/clock.svelte.js";
import { vocabulary } from "#lib/vocabulary.js";
import type { Live } from "#lib/admin/live.js";
import { relative } from "#lib/admin/words.js";
import ScanProgress from "#lib/components/admin/ScanProgress.svelte";
import { Progress } from "#lib/components/ui/progress/index.js";

// What the server is doing now, as the admin stream tells it: scans with their
// progress, scheduled tasks and when they started, and jobs by kind, each with
// how far its backlog has got, as Plex's activity panel does (a scan can queue
// thousands).
let { live, libraries }: { live: Live; libraries: Map<string, string> } =
	$props();

const clock = ticking();
const running = $derived(Object.groupBy(live.jobs, (j) => j.kind));
// A job running before its backlog is first told still has a line.
const jobs = $derived(
	[
		...new Set([
			...live.backlogs.map((b) => b.kind),
			...live.jobs.map((j) => j.kind),
		]),
	].map((kind) => {
		const backlog = live.backlogs.find((b) => b.kind === kind);
		return {
			kind,
			running: running[kind]?.length ?? 0,
			done: backlog?.done ?? 0,
			total: backlog ? backlog.done + backlog.left : 0,
		};
	}),
);

const words = vocabulary();
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
				<span class="text-ink">{words.tasks[task.key].name}</span>
				<span class="text-ink-3">
					started {relative(task.started_at, clock.now)}
				</span>
			</li>
		{/each}
	</ul>
{/if}
{#if jobs.length}
	<ul class="grid gap-3 text-sm" aria-label="Jobs running">
		{#each jobs as job (job.kind)}
			{@const name = words.jobs[job.kind]}
			{@const said = `${job.done.toLocaleString()} of ${job.total.toLocaleString()}`}
			<li class="grid gap-1.5">
				<p class="flex justify-between gap-4">
					<span class="text-ink">{name}</span>
					<span class="text-ink-3 tabular-nums">
						{job.total ? said : `${job.running} running`}
					</span>
				</p>
				{#if job.total}
					<Progress
						value={job.done}
						max={job.total}
						aria-label="{name}: {said}"
					/>
				{/if}
			</li>
		{/each}
	</ul>
{/if}
{#if !live.scans.length && !live.tasks.length && !jobs.length}
	<p class="text-ink-3 text-sm">
		{live.ready
			? "No scans, tasks or jobs are running."
			: "Waiting for live updates…"}
	</p>
{/if}
