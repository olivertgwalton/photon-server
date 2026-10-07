<script lang="ts">
import PageHeader from "#lib/components/PageHeader.svelte";
import { toast } from "svelte-sonner";
import { goto } from "$app/navigation";
import { fields } from "#lib/form.js";
import { libraryChange } from "#lib/admin/library.js";
import { client } from "#lib/api/client.js";
import { problemMessage } from "#lib/api/problem.js";
import LibraryForm from "#lib/components/admin/LibraryForm.svelte";
import { Button } from "#lib/components/ui/button/index.js";

let { data } = $props();

const api = client();
let adding = $state(false);

async function add(event: SubmitEvent) {
	const form = fields(event);
	adding = true;
	const added = await api.POST("/api/v1/admin/libraries", {
		body: {
			name: String(form.get("name") ?? ""),
			kind: form.get("kind") === "shows" ? "shows" : "movies",
			root: String(form.get("root") ?? ""),
		},
	});
	if (added.error) {
		adding = false;
		return toast.error(problemMessage(added.error));
	}
	// A new library starts as the server's defaults; what the form says
	// differently is set before its first scan has read much.
	const { error } = await api.PATCH("/api/v1/admin/libraries/{id}", {
		params: { path: { id: added.data.id } },
		body: libraryChange(form, added.data),
	});
	adding = false;
	if (error) toast.error(problemMessage(error));
	else toast.success(`${added.data.name} was added and is being scanned.`);
	await goto("/settings/server/libraries", { refreshAll: true });
}
</script>

<PageHeader
	title="Add a library"
	description="A folder of films or of shows. It is scanned as soon as it is added, and watched for changes after."
/>

<form onsubmit={add} class="grid max-w-2xl gap-6">
	<LibraryForm
		providers={data.providers}
		locales={data.locales}
		serverLanguage={data.serverLanguage}
	/>
	<div class="flex gap-2">
		<Button type="submit" disabled={adding}>Add and scan</Button>
		<Button href="/settings/server/libraries" variant="outline">Cancel</Button>
	</div>
</form>
