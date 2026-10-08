<script lang="ts">
import PageHeader from "#lib/components/PageHeader.svelte";
import { ticking } from "#lib/admin/clock.svelte.js";
import { liveStream } from "#lib/admin/stream.svelte.js";
import { loggedKinds } from "#lib/admin/words.js";
import ActivityList from "#lib/components/admin/ActivityList.svelte";
import Choice from "#lib/components/Choice.svelte";
import Pager from "#lib/components/Pager.svelte";
import { narrow } from "#lib/admin/narrow.js";
import { Label } from "#lib/components/ui/label/index.js";

let { data } = $props();

const live = liveStream();
const clock = ticking(30_000);

const kinds = [
	{ value: "all", label: "Everything" },
	...Object.entries(loggedKinds).map(([value, label]) => ({ value, label })),
];

// What arrived since the page loaded leads the first page, where it belongs.
const events = $derived(
	data.page.offset === 0
		? [
				...live.state.arrived.filter(
					(e) =>
						(!data.kind || e.kind === data.kind) &&
						!data.page.items.some((old) => old.id === e.id),
				),
				...data.page.items,
			]
		: data.page.items,
);
</script>

<PageHeader
	title="Activity"
	description="What has happened on the server, the latest first: sign-ins, plays, libraries and profiles added or removed."
>
	{#snippet actions()}
		<!-- Applied as it is chosen, as the library's filters are. -->
		<div class="grid gap-1.5">
			<Label for="kind">Show</Label>
			<Choice
				id="kind"
				name="kind"
				value={data.kind ?? "all"}
				options={kinds}
				onchange={(v: string) => narrow("kind", v)}
				class="w-48"
			/>
		</div>
	{/snippet}
</PageHeader>
<p class="text-sm">Kept for 30 days.</p>

<ActivityList {events} now={clock.now} />

<Pager offset={data.page.offset} limit={data.limit} total={data.page.total} />
