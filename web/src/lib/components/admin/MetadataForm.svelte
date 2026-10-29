<script lang="ts">
import LockIcon from "@lucide/svelte/icons/lock";
import LockOpenIcon from "@lucide/svelte/icons/lock-open";
import { SvelteSet } from "svelte/reactivity";
import { act } from "#lib/act.js";
import { editOf, fieldsOf } from "#lib/admin/edit.js";
import { client } from "#lib/api/client.js";
import type { components } from "#lib/api/schema.js";
import { Button } from "#lib/components/ui/button/index.js";
import * as Field from "#lib/components/ui/field/index.js";
import { Input } from "#lib/components/ui/input/index.js";
import { Textarea } from "#lib/components/ui/textarea/index.js";
import { fields as formFields } from "#lib/form.js";
import { getShell } from "#lib/shell.svelte.js";

const api = client();

type Schemas = components["schemas"];

// A title's own words, field by field, each with a padlock as Plex's Edit
// Details has. A field written or locked here is the admin's: no source
// changes it. Unlocking one lets the sources say it again.
let { title }: { title: Schemas["TitlePage"] } = $props();
const shell = getShell();

const id = $props.id();

const all: {
	key: Schemas["Field"];
	label: string;
	long?: boolean;
	type?: string;
	hint?: string;
	// Where a special aired is an episode's alone.
	episode?: boolean;
}[] = [
	{ key: "title", label: "Title" },
	{
		key: "sort_title",
		label: "Sort title",
		hint: "Sorted by the title where empty.",
	},
	{ key: "original_title", label: "Original title" },
	{ key: "tagline", label: "Tagline" },
	{ key: "overview", label: "Overview", long: true },
	{ key: "certificate", label: "Certificate" },
	{ key: "release_date", label: "Released", type: "date" },
	{ key: "year", label: "Year", type: "number" },
	{ key: "genres", label: "Genres", hint: "Separated by commas." },
	{ key: "studios", label: "Studios", hint: "Separated by commas." },
	{
		key: "airs_before_season",
		label: "Airs before season",
		type: "number",
		hint: "A special is watched in this season: before the episode below, or at its start.",
		episode: true,
	},
	{
		key: "airs_before_episode",
		label: "Airs before episode",
		type: "number",
		episode: true,
	},
	{
		key: "airs_after_season",
		label: "Airs after season",
		type: "number",
		hint: "A special is watched at the end of this season.",
		episode: true,
	},
];
const fields = $derived(
	all.filter((f) => !f.episode || title.kind === "episode"),
);

const values = $derived(fieldsOf(title));
const path = $derived({ params: { path: { id: title.id } } });
const locked = $derived(new Set(title.locked_fields ?? []));
// Fields to lock when the form is saved: those written in, as Plex locks a
// field as it is edited, and those locked by hand.
const locking = new SvelteSet<Schemas["Field"]>();
let refusal = $state("");

async function save(event: SubmitEvent) {
	const body = editOf(formFields(event), title);
	refusal = typeof body === "string" ? body : "";
	if (typeof body === "string") return;
	const saved = await act(
		api.PATCH("/api/v1/admin/titles/{id}", { ...path, body }),
		"Saved.",
	);
	if (saved) locking.clear();
}

function toggle(field: Schemas["Field"], label: string) {
	if (locking.has(field)) locking.delete(field);
	else if (!locked.has(field)) locking.add(field);
	else
		act(
			api.DELETE("/api/v1/admin/titles/{id}/edits", {
				params: { path: { id: title.id }, query: { field: [field] } },
			}),
			`${label} unlocked. The sources' ${label.toLowerCase()} returns at the next scan.`,
		);
}

const unlockAll = () =>
	shell.confirmFirst(
		"Unlock every field?",
		`Your edits to ${title.title} are dropped, and each field takes what the sources say again at the next scan.`,
		"Unlock all",
		() => act(api.DELETE("/api/v1/admin/titles/{id}/edits", path), "Unlocked."),
	);
</script>

<form onsubmit={save}>
	<Field.Group>
		{#each fields as field (field.key)}
			<Field.Field>
				{@const shut = locked.has(field.key) || locking.has(field.key)}
				<div class="flex items-center gap-2">
					<Field.Label for="{id}-field-{field.key}">{field.label}</Field.Label>
					<button
						type="button"
						aria-pressed={shut}
						aria-label="{shut ? "Unlock" : "Lock"} {field.label}"
						title={shut
							? "Refreshes leave it as it is. Unlock to take the sources' again."
							: "Lock it, so refreshes leave it as it is."}
						onclick={() => toggle(field.key, field.label)}
						class={[
							"inline-flex items-center gap-1 rounded-full text-xs outline-none focus-visible:ring-2 focus-visible:ring-signal",
							shut
								? "bg-accent text-ink hover:bg-accent/70 px-2 py-0.5"
								: "text-ink-3 hover:text-ink p-0.5",
						]}
					>
						{#if shut}
							<LockIcon class="size-3" />
							<span aria-hidden="true">Locked</span>
						{:else}
							<LockOpenIcon class="size-3.5" />
						{/if}
					</button>
					{#if locking.has(field.key)}
						<input type="hidden" name="locked" value={field.key}>
					{/if}
				</div>
				{#if field.long}
					<Textarea
						id="{id}-field-{field.key}"
						name={field.key}
						value={values[field.key]}
						rows={5}
						oninput={() => locking.add(field.key)}
					/>
				{:else}
					<Input
						id="{id}-field-{field.key}"
						name={field.key}
						type={field.type ?? "text"}
						value={values[field.key]}
						oninput={() => locking.add(field.key)}
					/>
				{/if}
				{#if field.hint}
					<Field.Description>{field.hint}</Field.Description>
				{/if}
			</Field.Field>
		{/each}
		<Field.Error errors={[{ message: refusal }]} />
		<Field.Field orientation="horizontal">
			<Button type="submit">Save</Button>
			{#if locked.size}
				<Button variant="outline" onclick={unlockAll}>Unlock all</Button>
			{/if}
		</Field.Field>
	</Field.Group>
</form>
