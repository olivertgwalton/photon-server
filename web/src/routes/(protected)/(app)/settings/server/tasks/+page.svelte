<script lang="ts">
import PageHeader from "#lib/components/PageHeader.svelte";
import { vocabulary } from "#lib/vocabulary.js";
import { runTask } from "#lib/actions.svelte.js";
import { runtime } from "#lib/format.js";
import PlayIcon from "@lucide/svelte/icons/play";
import { ticking } from "#lib/admin/clock.svelte.js";
import { liveStream } from "#lib/admin/stream.svelte.js";
import { relative, when } from "#lib/admin/words.js";
import { Badge } from "#lib/components/ui/badge/index.js";
import { Button } from "#lib/components/ui/button/index.js";
import * as Table from "#lib/components/ui/table/index.js";
import { act } from "#lib/act.js";
import { client } from "#lib/api/client.js";
import type { components } from "#lib/api/schema.js";
import Choice from "#lib/components/Choice.svelte";
import ConfirmButton from "#lib/components/admin/ConfirmButton.svelte";
import { Progress } from "#lib/components/ui/progress/index.js";
import * as Field from "#lib/components/ui/field/index.js";
import { fields } from "#lib/form.js";

let { data } = $props();
const words = vocabulary();

const api = client();

const live = liveStream();
const clock = ticking(30_000);

const hours = Array.from({ length: 24 }, (_, h) => ({
	value: String(h),
	label: `${String(h).padStart(2, "0")}:00`,
}));

const zones = $derived(
	[
		...new Set([
			data.maintenance.time_zone,
			"UTC",
			...Intl.supportedValuesOf("timeZone"),
		]),
	].map((z) => ({ value: z, label: z.replaceAll("_", " ") })),
);

const timings = [
	{ value: "window", label: "In the window" },
	{ value: "window_and_added", label: "In the window and as titles are added" },
] as const;

function saveWindow(event: SubmitEvent) {
	const form = fields(event);
	const timing = (name: string) =>
		String(form.get(name)) as components["schemas"]["Timing"];
	return act(
		api.PUT("/api/v1/admin/maintenance", {
			body: {
				start_hour: Number(form.get("start_hour")),
				end_hour: Number(form.get("end_hour")),
				time_zone: String(form.get("time_zone")),
				previews: timing("previews"),
				markers: timing("markers"),
			},
		}),
		"Saved.",
	);
}

// How far a task's work has got: the backlog of the jobs it queued, as the
// stream tells it, and how many of them run now.
function work(task: components["schemas"]["Task"]) {
	const kinds = task.jobs ?? [];
	const backlogs = live.state.backlogs.filter((b) => kinds.includes(b.kind));
	const done = backlogs.reduce((n, b) => n + b.done, 0);
	const total = backlogs.reduce((n, b) => n + b.done + b.left, 0);
	const running = live.state.jobs.filter((j) => kinds.includes(j.kind)).length;
	return {
		done,
		total,
		running,
		left: total - done,
		percent: total ? Math.floor((done / total) * 100) : 0,
	};
}

function took(started?: string, finished?: string) {
	if (!started || !finished) return "";
	const ms = Date.parse(finished) - Date.parse(started);
	return ms < 60_000 ? `${Math.round(ms / 1000)} s` : runtime(ms);
}
</script>

<PageHeader
	title="Scheduled tasks"
	description="What the server does by itself, and when it next will. Run one now to have it sooner: what it finds to do starts at once, outside the maintenance window too, and runs to its end."
/>

<form onsubmit={saveWindow} class="grid max-w-2xl gap-6">
	<Field.Set>
		<Field.Legend>Maintenance window</Field.Legend>
		<Field.Description>
			When previews are made and intros and credits are found. Nothing that
			reads the libraries' files in the background starts while anything plays.
		</Field.Description>
		<div class="grid gap-4 sm:grid-cols-3">
			<Field.Field>
				<Field.Label for="window-start">From</Field.Label>
				<Choice
					id="window-start"
					name="start_hour"
					value={String(data.maintenance.start_hour)}
					options={hours}
				/>
			</Field.Field>
			<Field.Field>
				<Field.Label for="window-end">Until</Field.Label>
				<Choice
					id="window-end"
					name="end_hour"
					value={String(data.maintenance.end_hour)}
					options={hours}
				/>
			</Field.Field>
			<Field.Field>
				<Field.Label for="window-zone">Time zone</Field.Label>
				<Choice
					id="window-zone"
					name="time_zone"
					value={data.maintenance.time_zone}
					options={zones}
				/>
			</Field.Field>
		</div>
		<div class="grid gap-4 sm:grid-cols-2">
			<Field.Field>
				<Field.Label for="window-previews">Make previews</Field.Label>
				<Choice
					id="window-previews"
					name="previews"
					value={data.maintenance.previews}
					options={timings}
				/>
			</Field.Field>
			<Field.Field>
				<Field.Label for="window-markers">Find intros and credits</Field.Label>
				<Choice
					id="window-markers"
					name="markers"
					value={data.maintenance.markers}
					options={timings}
				/>
			</Field.Field>
		</div>
	</Field.Set>
	<div><Button type="submit">Save</Button></div>
</form>

<Table.Root>
	<Table.Header>
		<Table.Row>
			<Table.Head>Task</Table.Head>
			<Table.Head>Last run</Table.Head>
			<Table.Head class="hidden sm:table-cell">Next run</Table.Head>
			<Table.Head><span class="sr-only">Run</span></Table.Head>
		</Table.Row>
	</Table.Header>
	<Table.Body>
		{#each data.tasks as task (task.key)}
			{@const running =
				task.running || live.state.tasks.some((t) => t.key === task.key)}
			{@const w = work(task)}
			<Table.Row>
				<Table.Cell class="whitespace-normal">
					<p class="text-ink font-semibold">{words.tasks[task.key].name}</p>
					<p class="text-ink-3 text-xs">{words.tasks[task.key].description}</p>
					{#if w.total}
						<div class="mt-2 grid max-w-sm gap-1">
							<Progress
								value={w.done}
								max={w.total}
								aria-label="{words.tasks[task.key].name}: {w.percent}%"
							/>
							<p class="text-ink-3 text-xs tabular-nums">
								{w.percent}% · {w.done.toLocaleString()} of
								{w.total.toLocaleString()}
								{#if w.running}
									· {w.running} running
								{/if}
							</p>
						</div>
					{:else if w.running}
						<p class="text-ink-3 mt-2 text-xs">{w.running} running</p>
					{/if}
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
					<!-- A phone has no room for a column of its own. -->
					<p class="text-ink-3 text-xs sm:hidden">
						Next {relative(task.next_at, clock.now)}
					</p>
				</Table.Cell>
				<Table.Cell class="hidden sm:table-cell">
					<time
						datetime={task.next_at}
						title={when.format(new Date(task.next_at))}
					>
						{relative(task.next_at, clock.now)}
					</time>
				</Table.Cell>
				<Table.Cell class="space-x-2 text-right whitespace-nowrap">
					{#if w.total || w.running}
						<ConfirmButton
							label="Stop"
							hidden={words.tasks[task.key].name}
							title="Stop {words.tasks[task.key].name.toLowerCase()}?"
							confirm="Stop"
							onconfirm={() =>
								act(
									api.POST("/api/v1/admin/tasks/{key}/stop", {
										params: { path: { key: task.key } },
									}),
									`${words.tasks[task.key].name} was stopped.`,
								)}
						>
							The {w.left.toLocaleString()} left are taken off the queue, and
							one running stops within a few minutes. What is done is kept: the
							task's next run, or Run now, takes up the rest.
						</ConfirmButton>
					{/if}
					<Button
						variant="outline"
						size="sm"
						disabled={running}
						onclick={() =>
							runTask(task.key, `${words.tasks[task.key].name} is running.`)}
					>
						<PlayIcon aria-hidden="true" />
						<span class="max-sm:sr-only">Run now</span>
						<span class="sr-only">{words.tasks[task.key].name}</span>
					</Button>
				</Table.Cell>
			</Table.Row>
		{/each}
	</Table.Body>
</Table.Root>
