<script lang="ts">
import { vocabulary } from "#lib/vocabulary.js";
import ListFilterIcon from "@lucide/svelte/icons/list-filter";
import type { components } from "#lib/api/schema.js";
import { Button } from "#lib/components/ui/button/index.js";
import * as DropdownMenu from "#lib/components/ui/dropdown-menu/index.js";
import { score } from "#lib/format.js";
import {
	filterCount,
	type ListFilter,
	toggled,
	type WallQuery,
} from "#lib/wall.js";

type Facets = components["schemas"]["Facets"];
type Site = components["schemas"]["RatingSite"];

let {
	query,
	facets,
	onchange,
}: {
	query: WallQuery;
	facets: Facets;
	onchange: (query: WallQuery) => void;
} = $props();
const words = vocabulary();

// A certificate is filtered by with its country (GB:PG) and shown as a viewer reads it.
const certificates = $derived(
	new Map(facets.certificates.map((c) => [c.value, c.name])),
);

// Each list a library has values for, as the server named them.
const groups = $derived(
	(
		[
			["Status", "mark", facets.marks, (v) => words.marks[v]],
			["Genre", "genre", facets.genres],
			["Year", "year", facets.years],
			[
				"Certificate",
				"certificate",
				[...certificates.keys()],
				(v) => certificates.get(String(v)) ?? String(v),
			],
			["Studio", "studio", facets.studios],
			[
				"Resolution",
				"resolution",
				facets.resolutions,
				(v) => words.resolutions[v],
			],
			["Dynamic range", "range", facets.ranges, (v) => words.ranges[v]],
		] as [
			string,
			ListFilter,
			(string | number)[],
			((v: string | number) => string)?,
		][]
	).filter(([, , values]) => values.length),
);

const thresholds = [50, 60, 70, 80, 90];
const site = $derived<Site>(query.rating_site ?? facets.rating_sites[0]);
const count = $derived(filterCount(query));

function chosen(name: ListFilter, value: string | number): boolean {
	return ((query[name] ?? []) as (string | number)[]).includes(value);
}
</script>

<DropdownMenu.Root>
	<DropdownMenu.Trigger>
		{#snippet child({
			props,
		})}
			<Button variant="outline" size="sm" {...props}>
				<ListFilterIcon />
				Filter
				{#if count}
					<span
						class="bg-ink text-ground rounded-full px-1.5 font-mono text-xs"
					>
						{count}
						<span class="sr-only">on</span>
					</span>
				{/if}
			</Button>
		{/snippet}
	</DropdownMenu.Trigger>
	<DropdownMenu.Content align="start" class="w-56">
		{#each groups as [title, name, values, label] (name)}
			<DropdownMenu.Sub>
				<DropdownMenu.SubTrigger>
					{title}
					{#if query[name]?.length}
						<span class="text-ink-3 ml-auto font-mono text-xs">
							{query[name]?.length}
						</span>
					{/if}
				</DropdownMenu.SubTrigger>
				<DropdownMenu.SubContent class="max-h-80 w-56 overflow-y-auto">
					{#each values as value (value)}
						<DropdownMenu.CheckboxItem
							checked={chosen(name, value)}
							onCheckedChange={() => onchange(toggled(query, name, value))}
						>
							{label ? label(value) : value}
						</DropdownMenu.CheckboxItem>
					{/each}
				</DropdownMenu.SubContent>
			</DropdownMenu.Sub>
		{/each}
		{#if facets.rating_sites.length}
			<DropdownMenu.Sub>
				<DropdownMenu.SubTrigger>Minimum rating</DropdownMenu.SubTrigger>
				<DropdownMenu.SubContent class="w-56">
					<DropdownMenu.RadioGroup
						value={String(query.min_rating ?? 0)}
						onValueChange={(value) =>
							onchange({
								...query,
								min_rating: Number(value) || undefined,
								rating_site:
									Number(value) || query.sort === "rating" ? site : undefined,
							})}
					>
						<DropdownMenu.RadioItem value="0" closeOnSelect={false}>
							Any
						</DropdownMenu.RadioItem>
						{#each thresholds as threshold (threshold)}
							<DropdownMenu.RadioItem
								value={String(threshold)}
								closeOnSelect={false}
							>
								{score(site, threshold)}
								or more
							</DropdownMenu.RadioItem>
						{/each}
					</DropdownMenu.RadioGroup>
					{#if facets.rating_sites.length > 1}
						<DropdownMenu.Separator />
						<DropdownMenu.Group>
							<DropdownMenu.GroupHeading>Rated by</DropdownMenu.GroupHeading>
							<DropdownMenu.RadioGroup
								value={site}
								onValueChange={(value) =>
									onchange({ ...query, rating_site: value as Site })}
							>
								{#each facets.rating_sites as value (value)}
									<DropdownMenu.RadioItem {value} closeOnSelect={false}>
										{words.rating_sites[value]}
									</DropdownMenu.RadioItem>
								{/each}
							</DropdownMenu.RadioGroup>
						</DropdownMenu.Group>
					{/if}
				</DropdownMenu.SubContent>
			</DropdownMenu.Sub>
		{/if}
	</DropdownMenu.Content>
</DropdownMenu.Root>
