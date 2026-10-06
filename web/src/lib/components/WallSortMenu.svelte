<script lang="ts">
import ArrowDownUpIcon from "@lucide/svelte/icons/arrow-down-up";
import type { components } from "#lib/api/schema.js";
import { Button } from "#lib/components/ui/button/index.js";
import * as DropdownMenu from "#lib/components/ui/dropdown-menu/index.js";
import { ratingSites } from "#lib/format.js";
import type { WallQuery } from "#lib/wall.js";

type Sort = components["schemas"]["WallSort"];
type Order = components["schemas"]["Order"];

let {
	query,
	sites,
	onchange,
}: {
	query: WallQuery;
	sites: components["schemas"]["RatingSite"][];
	onchange: (query: WallQuery) => void;
} = $props();

const sorts: Record<Sort, { name: string; asc: string; desc: string }> = {
	title: { name: "Title", asc: "A to Z", desc: "Z to A" },
	added: { name: "Date added", asc: "Oldest first", desc: "Newest first" },
	released: { name: "Release date", asc: "Oldest first", desc: "Newest first" },
	rating: { name: "Rating", asc: "Lowest first", desc: "Highest first" },
	runtime: { name: "Runtime", asc: "Shortest first", desc: "Longest first" },
	played: { name: "Last played", asc: "Longest ago", desc: "Most recent" },
};

const sort = $derived(query.sort ?? "title");
// As the server orders by default: titles from A, the rest newest first.
const order = $derived(query.order ?? (sort === "title" ? "asc" : "desc"));
const site = $derived(query.rating_site ?? "imdb");

function sortBy(value: string) {
	const next = value as Sort;
	onchange({
		...query,
		sort: next === "title" ? undefined : next,
		order: undefined,
		// The server rates by IMDb unless told; a library may have no IMDb scores.
		rating_site:
			next === "rating"
				? (query.rating_site ?? sites[0])
				: query.min_rating
					? query.rating_site
					: undefined,
	});
}
</script>

<DropdownMenu.Root>
	<DropdownMenu.Trigger>
		{#snippet child({
			props,
		})}
			<Button variant="outline" size="sm" {...props}>
				<ArrowDownUpIcon />
				<span>
					<span class="sr-only">Sort by</span>
					{sorts[sort].name}{sort === "rating" ? ` (${ratingSites[site]})` : ""}
				</span>
			</Button>
		{/snippet}
	</DropdownMenu.Trigger>
	<DropdownMenu.Content align="start" class="w-56">
		<DropdownMenu.Group>
			<DropdownMenu.GroupHeading>Sort by</DropdownMenu.GroupHeading>
			<DropdownMenu.RadioGroup value={sort} onValueChange={sortBy}>
				{#each Object.entries(sorts) as [value, { name }] (value)}
					{#if value !== "rating" || sites.length}
						<DropdownMenu.RadioItem {value}>{name}</DropdownMenu.RadioItem>
					{/if}
				{/each}
			</DropdownMenu.RadioGroup>
		</DropdownMenu.Group>
		<DropdownMenu.Separator />
		<DropdownMenu.Group>
			<DropdownMenu.GroupHeading>Order</DropdownMenu.GroupHeading>
			<DropdownMenu.RadioGroup
				value={order}
				onValueChange={(value) => onchange({ ...query, order: value as Order })}
			>
				<DropdownMenu.RadioItem value="asc">
					{sorts[sort].asc}
				</DropdownMenu.RadioItem>
				<DropdownMenu.RadioItem value="desc">
					{sorts[sort].desc}
				</DropdownMenu.RadioItem>
			</DropdownMenu.RadioGroup>
		</DropdownMenu.Group>
		{#if sort === "rating" && sites.length > 1}
			<DropdownMenu.Separator />
			<DropdownMenu.Group>
				<DropdownMenu.GroupHeading>Rated by</DropdownMenu.GroupHeading>
				<DropdownMenu.RadioGroup
					value={site}
					onValueChange={(value) =>
						onchange({
							...query,
							rating_site: value as components["schemas"]["RatingSite"],
						})}
				>
					{#each sites as value (value)}
						<DropdownMenu.RadioItem {value}>
							{ratingSites[value]}
						</DropdownMenu.RadioItem>
					{/each}
				</DropdownMenu.RadioGroup>
			</DropdownMenu.Group>
		{/if}
	</DropdownMenu.Content>
</DropdownMenu.Root>
