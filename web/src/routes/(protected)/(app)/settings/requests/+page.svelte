<script lang="ts">
import { act } from "#lib/act.js";
import { withQuery } from "#lib/address.js";
import { when } from "#lib/admin/words.js";
import { client } from "#lib/api/client.js";
import type { components } from "#lib/api/schema.js";
import Choice from "#lib/components/Choice.svelte";
import PageHeader from "#lib/components/PageHeader.svelte";
import Pager from "#lib/components/Pager.svelte";
import * as AlertDialog from "#lib/components/ui/alert-dialog/index.js";
import { Button } from "#lib/components/ui/button/index.js";
import * as Field from "#lib/components/ui/field/index.js";
import { Textarea } from "#lib/components/ui/textarea/index.js";
import { vocabulary } from "#lib/vocabulary.js";
import { goto } from "$app/navigation";
import { page } from "$app/state";

type Request = components["schemas"]["Request"];

let { data } = $props();
const words = vocabulary();
const api = client();
const id = $props.id();

const states = [
	...(
		["pending", "failed", "approved", "sent", "declined", "available"] as const
	).map((s) => ({ value: s, label: words.request_states[s] })),
	{ value: "all", label: "Every request" },
];

function show(state: string) {
	goto(withQuery(page.url, { state, offset: undefined }), { replace: true });
}

const approve = (r: Request) =>
	act(
		api.POST("/api/v1/admin/requests/{id}/approve", {
			params: { path: { id: r.id } },
		}),
		`${r.title} is approved.`,
	);

let declining = $state<Request>();
let reason = $state("");
const decline = (r: Request) =>
	act(
		api.POST("/api/v1/admin/requests/{id}/decline", {
			params: { path: { id: r.id } },
			body: { reason: reason.trim() },
		}),
		`${r.title} is declined.`,
	);

// Those waiting, and those that failed where they were sent, are decided
// here; a failed one approved again is sent again.
const decidable = (r: Request) => r.state === "pending" || r.state === "failed";
const { offset, total } = $derived(data.requests);
</script>

<PageHeader
	title="Requests"
	description="Titles people asked for that your libraries don't have. Approved, a request goes where its library sends its requests, or waits for you to add the title yourself; whoever asked is told once it's added."
/>

<Choice
	aria-label="Show"
	class="w-56"
	value={data.state}
	options={states}
	onchange={show}
/>

{#if data.requests.items.length}
	<ul class="grid gap-3">
		{#each data.requests.items as r (r.id)}
			<li
				class="bg-raise grid gap-2 rounded-xl p-4 sm:grid-cols-[1fr_auto] sm:items-start"
			>
				<div class="grid min-w-0 gap-1">
					<p class="text-ink font-semibold">
						{#if r.title_id}
							<a href="/titles/{r.title_id}" class="hover:underline"
								>{r.title}</a
							>
						{:else}
							{r.title}
						{/if}
						{#if r.year}
							<span class="text-ink-3 font-normal"> ({r.year})</span>
						{/if}
						<span class="label ml-2">{words.request_states[r.state]}</span>
					</p>
					<p class="text-ink-3 text-xs">
						{r.kind === "show"
							? r.seasons?.length
								? `Seasons ${r.seasons.join(", ")}`
								: "Every season"
							: "Film"}
						· asked by {r.profile}
						{when.format(new Date(r.asked_at))}
					</p>
					{#if r.reason}
						<p
							class={[
								"text-xs wrap-break-word",
								r.state === "failed"
									? "text-destructive font-mono"
									: "text-ink-2",
							]}
						>
							{r.reason}
						</p>
					{/if}
				</div>
				{#if decidable(r)}
					<div class="flex gap-2">
						<Button size="sm" onclick={() => approve(r)}>
							{r.state === "failed" ? "Approve again" : "Approve"}
							<span class="sr-only">{r.title}</span>
						</Button>
						<Button
							variant="outline"
							size="sm"
							onclick={() => {
								reason = "";
								declining = r;
							}}
						>
							Decline <span class="sr-only">{r.title}</span>
						</Button>
					</div>
				{/if}
			</li>
		{/each}
	</ul>
{:else}
	<p class="text-ink-3 text-sm">No requests here.</p>
{/if}
<Pager {offset} {total} />

<AlertDialog.Root
	bind:open={
		() => !!declining,
		(open) => {
			if (!open) declining = undefined;
		}
	}
>
	<AlertDialog.Content>
		<AlertDialog.Header>
			<AlertDialog.Title>Decline {declining?.title}?</AlertDialog.Title>
			<AlertDialog.Description>
				{declining?.profile}
				sees that it was declined, and why, if you say.
			</AlertDialog.Description>
		</AlertDialog.Header>
		<Field.Field>
			<Field.Label for="{id}-reason">Why</Field.Label>
			<Textarea
				id="{id}-reason"
				bind:value={reason}
				rows={2}
				placeholder="Optional"
			/>
		</Field.Field>
		<AlertDialog.Footer>
			<AlertDialog.Cancel>Cancel</AlertDialog.Cancel>
			<AlertDialog.Action
				onclick={() => {
					const r = declining;
					declining = undefined;
					if (r) decline(r);
				}}
			>
				Decline
			</AlertDialog.Action>
		</AlertDialog.Footer>
	</AlertDialog.Content>
</AlertDialog.Root>
