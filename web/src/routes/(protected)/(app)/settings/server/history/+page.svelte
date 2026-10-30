<script lang="ts">
import { narrow } from "#lib/admin/narrow.ts";
import { byName, when } from "#lib/admin/words.ts";
import Choice from "#lib/components/Choice.svelte";
import CursorPager from "#lib/components/CursorPager.svelte";
import PageHeader from "#lib/components/PageHeader.svelte";
import { Button } from "#lib/components/ui/button/index.ts";
import { Label } from "#lib/components/ui/label/index.ts";
import * as Table from "#lib/components/ui/table/index.ts";
import { fullTitle } from "#lib/format.ts";
import { vocabulary } from "#lib/vocabulary.ts";

let { data } = $props();
const words = vocabulary();

const names = $derived(byName(data.profiles));
const everyone = $derived([
	{ value: "all", label: "Everyone" },
	...data.profiles.map((p) => ({ value: p.id, label: p.name })),
]);

// The file of whoever is chosen, as the table shows them.
const file = $derived(
	data.profile
		? `/api/v1/admin/history/file?profile=${encodeURIComponent(data.profile)}`
		: "/api/v1/admin/history/file",
);

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
		<Button href={file} download variant="outline" class="self-end"
			>Download history</Button
		>
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
							{fullTitle(entry.title, entry.title.show?.title)}
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

<CursorPager next={data.page.next} />
