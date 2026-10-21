<script lang="ts">
import { act } from "#lib/act.js";
import { client } from "#lib/api/client.js";
import Pager from "#lib/components/Pager.svelte";
import RequestTitle from "#lib/components/RequestTitle.svelte";
import { Button } from "#lib/components/ui/button/index.js";
import { askerState } from "#lib/requests.js";
import { vocabulary } from "#lib/vocabulary.js";

let { data } = $props();
const words = vocabulary();
const day = new Intl.DateTimeFormat(undefined, { dateStyle: "medium" });

const takeBack = (id: string, title: string) =>
	act(
		client().DELETE("/api/v1/requests/{id}", { params: { path: { id } } }),
		`You no longer ask for ${title}.`,
	);
const { offset, total } = $derived(data.requests);
</script>

<svelte:head><title>Requests · Photon</title></svelte:head>

<div class="grid max-w-4xl gap-8">
	<div class="grid gap-1">
		<h1 class="title">Requests</h1>
		<p class="text-ink-3">
			What you've asked for. You're notified when each one is added, as with
			anything new.
		</p>
	</div>
	<ul class="divide-line divide-y">
		{#each data.requests.items as r (r.id)}
			<li class="flex flex-wrap items-center gap-x-4 gap-y-2 py-3">
				<div class="min-w-0 flex-1">
					<p class="text-ink font-semibold">
						<RequestTitle request={r} />
					</p>
					<p class="text-ink-3 text-sm">
						{words.request_states[askerState(r.state)]}
						· asked {day.format(new Date(r.asked_at))}
						{#if r.state === "declined" && r.reason}
							· “{r.reason}”
						{/if}
					</p>
				</div>
				{#if r.state === "pending"}
					<Button
						variant="outline"
						size="sm"
						aria-label="Take back {r.title}"
						onclick={() => takeBack(r.id, r.title)}
					>
						Take back
					</Button>
				{/if}
			</li>
		{:else}
			<p class="text-ink-3">Nothing asked for yet.</p>
		{/each}
	</ul>
	<Pager {offset} total={total ?? 0} />
</div>
