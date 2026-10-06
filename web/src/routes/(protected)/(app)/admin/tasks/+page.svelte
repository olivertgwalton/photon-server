<script lang="ts">
import PlayIcon from "@lucide/svelte/icons/play";
import { ticking } from "#lib/admin/clock.svelte.js";
import { liveStream } from "#lib/admin/stream.svelte.js";
import { relative, tasks, when } from "#lib/admin/words.js";
import { Badge } from "#lib/components/ui/badge/index.js";
import { Button } from "#lib/components/ui/button/index.js";
import * as Table from "#lib/components/ui/table/index.js";
import { act } from "#lib/admin/act.js";
import { client } from "#lib/api/client.js";

let { data } = $props();

const api = client();

const live = liveStream();
const clock = ticking(30_000);

function took(started?: string, finished?: string) {
	if (!started || !finished) return "";
	const s = Math.round((Date.parse(finished) - Date.parse(started)) / 1000);
	return s < 60 ? `${s} s` : `${Math.round(s / 60)} min`;
}
</script>

<svelte:head><title>Tasks · Dashboard · Photon</title></svelte:head>

<h1 class="title">Scheduled tasks</h1>

<Table.Root>
	<Table.Header>
		<Table.Row>
			<Table.Head>Task</Table.Head>
			<Table.Head>Last run</Table.Head>
			<Table.Head>Next run</Table.Head>
			<Table.Head><span class="sr-only">Run</span></Table.Head>
		</Table.Row>
	</Table.Header>
	<Table.Body>
		{#each data.tasks as task (task.key)}
			{@const running =
				task.running || live.state.tasks.some((t) => t.key === task.key)}
			<Table.Row>
				<Table.Cell class="whitespace-normal">
					<p class="text-ink font-semibold">{tasks[task.key].name}</p>
					<p class="text-ink-3 text-xs">{tasks[task.key].does}</p>
				</Table.Cell>
				<Table.Cell class="whitespace-normal">
					{#if running}
						<Badge>Running</Badge>
					{:else if task.finished_at}
						<p>
							<time
								datetime={task.finished_at}
								title={when.format(new Date(task.finished_at))}
							>
								{relative(task.finished_at, clock.now)}
							</time>
							· {took(task.started_at, task.finished_at)}
						</p>
						{#if task.result === "failed"}
							<p class="text-destructive text-xs">Failed: {task.error}</p>
						{:else if task.result === "cancelled"}
							<p class="text-ink-3 text-xs">Cancelled</p>
						{/if}
					{:else}
						Never
					{/if}
				</Table.Cell>
				<Table.Cell>
					<time
						datetime={task.next_at}
						title={when.format(new Date(task.next_at))}
					>
						{relative(task.next_at, clock.now)}
					</time>
				</Table.Cell>
				<Table.Cell class="text-right">
					<Button
						variant="outline"
						size="sm"
						disabled={running}
						onclick={() =>
							act(
								api.POST("/api/v1/admin/tasks/{key}/run", {
									params: { path: { key: task.key } },
								}),
								`${tasks[task.key].name} is running.`,
							)}
					>
						<PlayIcon aria-hidden="true" />
						Run now <span class="sr-only">{tasks[task.key].name}</span>
					</Button>
				</Table.Cell>
			</Table.Row>
		{/each}
	</Table.Body>
</Table.Root>
