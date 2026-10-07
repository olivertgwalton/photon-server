<script lang="ts">
import { toast } from "svelte-sonner";
import { client } from "#lib/api/client.js";
import { problemMessage } from "#lib/api/problem.js";
import type { components } from "#lib/api/schema.js";
import { editOf, fieldsOf } from "#lib/admin/edit.js";
import { Button } from "#lib/components/ui/button/index.js";
import { Checkbox } from "#lib/components/ui/checkbox/index.js";
import * as Field from "#lib/components/ui/field/index.js";
import { Input } from "#lib/components/ui/input/index.js";
import { Label } from "#lib/components/ui/label/index.js";
import { Textarea } from "#lib/components/ui/textarea/index.js";
import { act } from "#lib/act.js";
import { fields as formFields } from "#lib/form.js";

const api = client();

type Schemas = components["schemas"];

// A title's own words, field by field. What an admin writes outranks every
// source and survives refreshes; holding a field keeps it as it is without
// writing it; giving it back lets the sources say it again.
let { title }: { title: Schemas["TitlePage"] } = $props();

const fields: {
	key: Schemas["Field"];
	label: string;
	long?: boolean;
	type?: string;
	hint?: string;
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
];

const values = $derived(fieldsOf(title));
const path = $derived({ params: { path: { id: title.id } } });
let refusal = $state("");

function save(event: SubmitEvent) {
	const body = editOf(formFields(event), title);
	refusal = typeof body === "string" ? body : "";
	if (typeof body === "string") return;
	return act(
		api.PATCH("/api/v1/admin/titles/{id}", { ...path, body }),
		"Saved.",
	);
}

async function giveBack(field: Schemas["Field"], label: string) {
	const { error } = await api.DELETE("/api/v1/admin/titles/{id}/edits", {
		params: { path: { id: title.id }, query: { field: [field] } },
	});
	if (error) toast.error(problemMessage(error));
	else toast.success(`${label} goes back to its sources at the next match.`);
}
</script>

<form onsubmit={save}>
	<Field.Group>
		{#each fields as field (field.key)}
			<Field.Field>
				<div class="flex flex-wrap items-center justify-between gap-2">
					<Field.Label for="field-{field.key}">{field.label}</Field.Label>
					<div class="flex items-center gap-3">
						<div class="flex items-center gap-1.5">
							<Checkbox id="hold-{field.key}" name="locked" value={field.key} />
							<Label for="hold-{field.key}" class="text-ink-3 text-xs">
								Hold <span class="sr-only">{field.label}</span>
							</Label>
						</div>
						<button
							type="button"
							onclick={() => giveBack(field.key, field.label)}
							class="text-ink-3 hover:text-ink text-xs underline-offset-4 hover:underline"
						>
							Give back
							<span class="sr-only">{field.label} to its sources</span>
						</button>
					</div>
				</div>
				{#if field.long}
					<Textarea
						id="field-{field.key}"
						name={field.key}
						value={values[field.key]}
						rows={5}
					/>
				{:else}
					<Input
						id="field-{field.key}"
						name={field.key}
						type={field.type ?? "text"}
						value={values[field.key]}
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
			<Button
				variant="outline"
				onclick={() =>
					act(
						api.DELETE("/api/v1/admin/titles/{id}/edits", path),
						"Every field goes back to its sources at the next match.",
					)}
			>
				Give every field back
			</Button>
		</Field.Field>
	</Field.Group>
</form>
