<script lang="ts">
import type { components } from "#lib/api/schema.js";
import { relative, when } from "#lib/admin/words.js";

type Event = components["schemas"]["Event"];

// Events as the activity log tells them, the newest first.
let { events, now }: { events: Event[]; now: number } = $props();

const alarming = new Set<Event["kind"]>([
	"task.failed",
	"job.dead",
	"auth.sign_in_refused",
]);
</script>

<ul class="divide-line divide-y">
	{#each events as e, i (e.id ?? i)}
		<li class="grid gap-0.5 py-2.5">
			<p class={alarming.has(e.kind) ? "text-destructive" : "text-ink"}>
				{#if e.title_id}
					<a href="/titles/{e.title_id}" class="hover:underline">{e.text}</a>
				{:else}
					{e.text}
				{/if}
			</p>
			<p class="text-ink-3 text-xs">
				<time datetime={e.at} title={when.format(new Date(e.at))}>
					{relative(e.at, now)}
				</time>
			</p>
		</li>
	{:else}
		<li class="text-ink-3 py-2.5 text-sm">Nothing has happened yet.</li>
	{/each}
</ul>
