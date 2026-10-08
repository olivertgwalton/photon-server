<script lang="ts">
import PageHeader from "#lib/components/PageHeader.svelte";
import { vocabulary } from "#lib/vocabulary.js";
import type { components } from "#lib/api/schema.js";
import { Button } from "#lib/components/ui/button/index.js";
import JobsTable from "#lib/components/admin/JobsTable.svelte";
import { act } from "#lib/act.js";
import { client } from "#lib/api/client.js";

type Schemas = components["schemas"];

let { data } = $props();
const words = vocabulary();

const api = client();

const kinds = $derived(
	(Object.keys(words.jobs) as Schemas["JobKind"][]).filter((kind) =>
		data.counts.some((c) => c.kind === kind),
	),
);

// Where a dead job's subject can be looked at: a scan's library, or a title.
function subject(job: Schemas["DeadJob"]) {
	switch (job.kind) {
		case "scan_library":
			return `/settings/server/libraries/${job.subject}`;
		case "identify":
			return `/titles/${job.subject}`;
		default:
			return undefined;
	}
}
</script>

<PageHeader
	title="Jobs"
	description="The work queued behind scans and plays: matching titles, finding intros, making previews and conversions, sending webhooks. A job that keeps failing is given up on, and waits here to be tried again."
/>

{#if kinds.length}
	<JobsTable counts={data.counts} {kinds} />
{:else}
	<p class="text-ink-3 text-sm">The queue is empty.</p>
{/if}

<section aria-labelledby="dead" class="grid gap-3">
	<h2 id="dead" class="heading">Given up</h2>
	{#if data.dead.length}
		<ul class="grid gap-3">
			{#each data.dead as job (job.id)}
				{@const href = subject(job)}
				<li
					class="bg-raise grid gap-2 rounded-xl p-4 sm:grid-cols-[1fr_auto] sm:items-start"
				>
					<div class="grid min-w-0 gap-1">
						<p class="text-ink font-semibold">
							{words.jobs[job.kind]}
							{#if href}
								·
								<a {href} class="underline-offset-4 hover:underline"
									>see what it was for</a
								>
							{/if}
						</p>
						<p class="text-ink-3 text-xs">After {job.attempts} tries</p>
						{#if job.error}
							<p class="text-destructive font-mono text-xs wrap-break-word">
								{job.error}
							</p>
						{/if}
					</div>
					<Button
						variant="outline"
						size="sm"
						onclick={() =>
							act(
								api.POST("/api/v1/admin/jobs/{id}/retry", {
									params: { path: { id: job.id } },
								}),
								"The job is queued again.",
							)}
					>
						Try again <span class="sr-only">job {job.id}</span>
					</Button>
				</li>
			{/each}
		</ul>
	{:else}
		<p class="text-ink-3 text-sm">Nothing has been given up on.</p>
	{/if}
</section>
