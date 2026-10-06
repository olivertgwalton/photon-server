<script lang="ts">
import PageHeader from "#lib/components/PageHeader.svelte";
import { page } from "$app/state";
import { liveStream } from "#lib/admin/stream.svelte.js";
import { loggedKinds } from "#lib/admin/words.js";
import ActivityList from "#lib/components/admin/ActivityList.svelte";
import Choice from "#lib/components/admin/Choice.svelte";
import Pager from "#lib/components/admin/Pager.svelte";
import { Button } from "#lib/components/ui/button/index.js";
import { Label } from "#lib/components/ui/label/index.js";

let { data } = $props();

const live = liveStream();
const now = Date.now();

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
const profiles = $derived(
	new Map(data.profiles.map((p) => [p.id, p.name] as const)),
);
const libraries = $derived(
	new Map(data.libraries.map((l) => [l.id, l.name] as const)),
);
</script>

<PageHeader
	title="Activity"
	description="What has happened on the server, the latest first: sign-ins, plays, libraries and profiles added or removed."
>
	{#snippet actions()}
		<form method="get" class="flex items-end gap-2">
			<div class="grid gap-1.5">
				<Label for="kind">Show</Label>
				<Choice
					id="kind"
					name="kind"
					value={data.kind ?? "all"}
					options={kinds}
					class="w-48"
				/>
			</div>
			<Button type="submit" variant="outline">Filter</Button>
		</form>
	{/snippet}
</PageHeader>
<p class="text-sm">Kept for 30 days.</p>

<ActivityList {events} {profiles} {libraries} {now} />

<Pager
	url={page.url}
	offset={data.page.offset}
	limit={data.limit}
	total={data.page.total}
/>
