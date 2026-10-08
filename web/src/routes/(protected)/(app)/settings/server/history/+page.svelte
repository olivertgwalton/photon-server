<script lang="ts">
import { vocabulary } from "#lib/vocabulary.js";
import { fullTitle } from "#lib/format.js";
import PageHeader from "#lib/components/PageHeader.svelte";
import { byName, when } from "#lib/admin/words.js";
import Choice from "#lib/components/Choice.svelte";
import Pager from "#lib/components/Pager.svelte";
import { narrow } from "#lib/admin/narrow.js";
import { Label } from "#lib/components/ui/label/index.js";
import * as Table from "#lib/components/ui/table/index.js";

let { data } = $props();
const words = vocabulary();

const names = $derived(byName(data.profiles));
const everyone = $derived([
	{ value: "all", label: "Everyone" },
	...data.profiles.map((p) => ({ value: p.id, label: p.name })),
]);

function name(card: (typeof data.page.items)[number]["title"]) {
	return fullTitle(
		card,
		card.kind === "episode" ? card.show?.title : undefined,
	);
}

function reached(position: number, duration?: number) {
	return duration
		? `${Math.min(Math.round((position / duration) * 100), 100)}%`
		: "";
}
</script>

<PageHeader
	title="Play history"
	description="Every play on the server, the latest first, and how it was played."
>
	{#snippet actions()}
		<!-- Applied as it is chosen, as the library's filters are. -->
		<div class="grid gap-1.5">
			<Label for="profile">Who</Label>
			<Choice
				id="profile"
				name="profile"
				value={data.profile ?? "all"}
				options={everyone}
				onchange={(v: string) => narrow("profile", v)}
				class="w-48"
			/>
		</div>
	{/snippet}
</PageHeader>

{#if data.page.items.length}
	<Table.Root>
		<Table.Header>
			<Table.Row>
				<Table.Head>Title</Table.Head>
				<Table.Head>Who</Table.Head>
				<Table.Head>How</Table.Head>
				<Table.Head>When</Table.Head>
				<Table.Head class="text-right">Reached</Table.Head>
			</Table.Row>
		</Table.Header>
		<Table.Body>
			{#each data.page.items as entry (entry.id)}
				<Table.Row>
					<Table.Cell class="max-w-sm truncate">
						<a
							href="/titles/{entry.title.id}"
							class="text-ink font-semibold hover:underline"
						>
							{name(entry.title)}
						</a>
					</Table.Cell>
					<Table.Cell
						>{names.get(entry.profile_id) ?? "A removed profile"}</Table.Cell
					>
					<Table.Cell>{words.play_methods[entry.method]}</Table.Cell>
					<Table.Cell>{when.format(new Date(entry.started_at))}</Table.Cell>
					<Table.Cell class="text-right font-mono">
						{reached(entry.position_ms, entry.title.duration_ms)}
					</Table.Cell>
				</Table.Row>
			{/each}
		</Table.Body>
	</Table.Root>
{:else}
	<p class="text-ink-3 text-sm">Nothing has been played yet.</p>
{/if}

<Pager offset={data.page.offset} limit={data.limit} total={data.page.total} />
