<script lang="ts">
import PlusIcon from "@lucide/svelte/icons/plus";
import XIcon from "@lucide/svelte/icons/x";
import type { components } from "#lib/api/schema.js";
import { markerKinds } from "#lib/admin/words.js";
import { Button } from "#lib/components/ui/button/index.js";
import { Checkbox } from "#lib/components/ui/checkbox/index.js";
import * as Field from "#lib/components/ui/field/index.js";
import { Input } from "#lib/components/ui/input/index.js";
import { Label } from "#lib/components/ui/label/index.js";
import { act } from "#lib/act.js";
import { fields } from "#lib/form.js";
import { markersOf } from "#lib/admin/edit.js";
import { client } from "#lib/api/client.js";
import { timecode } from "#lib/format.js";
import Choice from "./Choice.svelte";

type Schemas = components["schemas"];

// Where a copy's intro, credits, recap and preview are, over what its
// chapters, sound or picture say, and which of its parts have none of a kind.
// Saving with nothing listed gives the copy back to what was found.
let { version }: { version: Schemas["VersionPage"] } = $props();

let refusal = $state("");

function save(event: SubmitEvent) {
	const body = markersOf(fields(event));
	refusal = typeof body === "string" ? body : "";
	if (typeof body === "string") return;
	return act(
		client().PUT("/api/v1/admin/versions/{id}/markers", {
			params: { path: { id: version.id } },
			body,
		}),
		"Markers saved.",
	);
}

const sources = {
	user: "set here",
	chapter: "from a chapter",
	fingerprint: "found by sound",
	blackframes: "found by picture",
};
const kindOptions = Object.entries(markerKinds).map(([value, label]) => ({
	value: value as Schemas["MarkerKind"],
	label,
}));

let rows = $state<
	{
		key: number;
		kind: Schemas["MarkerKind"];
		start: string;
		end: string;
		source?: string;
	}[]
>([]);
let next = 0;
$effect.pre(() => {
	rows = (version.markers ?? []).map((m) => ({
		key: next++,
		kind: m.kind,
		start: timecode(m.start_ms),
		end: timecode(m.end_ms),
		source: sources[m.source],
	}));
});

const parts = $derived(Array.from({ length: version.parts }, (_, i) => i));
</script>

<form onsubmit={save} class="grid gap-4">
	{#if rows.length}
		<ul class="grid gap-2">
			{#each rows as row, i (row.key)}
				<li class="grid grid-cols-[1fr_5.5rem_5.5rem_auto] items-end gap-2">
					<Field.Field>
						<Field.Label for="kind-{row.key}" class={i ? "sr-only" : ""}
							>Kind</Field.Label
						>
						<Choice
							id="kind-{row.key}"
							name="kind"
							bind:value={row.kind}
							options={kindOptions}
							class="w-full"
						/>
					</Field.Field>
					<Field.Field>
						<Field.Label for="start-{row.key}" class={i ? "sr-only" : ""}
							>Starts</Field.Label
						>
						<Input
							id="start-{row.key}"
							name="start"
							bind:value={row.start}
							required
							class="font-mono"
						/>
					</Field.Field>
					<Field.Field>
						<Field.Label for="end-{row.key}" class={i ? "sr-only" : ""}
							>Ends</Field.Label
						>
						<Input
							id="end-{row.key}"
							name="end"
							bind:value={row.end}
							required
							class="font-mono"
						/>
					</Field.Field>
					<Button
						variant="ghost"
						size="icon"
						aria-label="Take out the {markerKinds[
							row.kind
						].toLowerCase()} at {row.start}"
						onclick={() => (rows = rows.filter((r) => r.key !== row.key))}
					>
						<XIcon />
					</Button>
					{#if row.source}
						<p class="text-ink-3 col-span-4 -mt-1 text-xs">{row.source}</p>
					{/if}
				</li>
			{/each}
		</ul>
	{:else}
		<p class="text-ink-3 text-sm">
			No intro, credits, recap or preview is marked.
		</p>
	{/if}
	<Button
		variant="outline"
		size="sm"
		class="justify-self-start"
		onclick={() =>
			rows.push({ key: next++, kind: "intro", start: "0:00", end: "1:00" })}
	>
		<PlusIcon aria-hidden="true" />
		Add a stretch
	</Button>
	<Field.Set>
		<Field.Legend>Has none</Field.Legend>
		<Field.Description>
			Say a part has no intro or credits, so nothing found by sound or picture
			is used for it.
		</Field.Description>
		{#each parts as part (part)}
			<div class="flex flex-wrap gap-x-5 gap-y-2">
				{#if parts.length > 1}
					<span class="label self-center">Part {part + 1}</span>
				{/if}
				{#each kindOptions as option (option.value)}
					<div class="flex items-center gap-2">
						<Checkbox
							id="absent-{version.id}-{part}-{option.value}"
							name="absent"
							value="{option.value}:{part}"
						/>
						<Label for="absent-{version.id}-{part}-{option.value}">
							No {option.label.toLowerCase()}
							{#if parts.length > 1}
								<span class="sr-only">in part {part + 1}</span>
							{/if}
						</Label>
					</div>
				{/each}
			</div>
		{/each}
	</Field.Set>
	<Field.Error errors={[{ message: refusal }]} />
	<Button type="submit" class="justify-self-start">Save markers</Button>
</form>
