<script lang="ts">
import { type EditorTab, editor } from "#lib/actions.svelte.js";
import { client } from "#lib/api/client.js";
import { problemMessage } from "#lib/api/problem.js";
import type { components } from "#lib/api/schema.js";
import ArtworkPicker from "#lib/components/admin/ArtworkPicker.svelte";
import IdentifyPanel from "#lib/components/admin/IdentifyPanel.svelte";
import MetadataForm from "#lib/components/admin/MetadataForm.svelte";
import * as Dialog from "#lib/components/ui/dialog/index.js";
import * as Tabs from "#lib/components/ui/tabs/index.js";

type Schemas = components["schemas"];

// A title's editing over the page it was opened from, as Plex's Edit dialog
// is: its details, its match and its pictures, each the admin title page's.
let title = $state<Schemas["TitlePage"]>();
let providers = $state<Schemas["MetadataProvider"][]>([]);
let failed = $state("");

// Asked each time it opens, so what was edited since is there.
$effect(() => {
	if (!editor.open) return;
	const api = client();
	const path = { params: { path: { id: editor.id } } };
	title = undefined;
	failed = "";
	Promise.all([
		api.GET("/api/v1/titles/{id}", path),
		api.GET("/api/v1/admin/providers"),
	]).then(([t, p]) => {
		if (t.data) title = t.data;
		else failed = problemMessage(t.error);
		providers = p.data?.items ?? [];
	});
});

const matched = $derived(title?.kind === "movie" || title?.kind === "show");
</script>

<Dialog.Root bind:open={editor.open}>
	<Dialog.Content class="max-h-[85svh] overflow-y-auto sm:max-w-3xl">
		<Dialog.Header>
			<Dialog.Title>Edit {title?.title ?? ""}</Dialog.Title>
		</Dialog.Header>
		{#if failed}
			<p role="alert" class="text-destructive">{failed}</p>
		{:else if !title}
			<p class="text-ink-3">Loading…</p>
		{:else}
			<Tabs.Root
				value={editor.tab}
				onValueChange={(tab) => (editor.tab = tab as EditorTab)}
			>
				<Tabs.List>
					<Tabs.Trigger value="details">Details</Tabs.Trigger>
					{#if matched}
						<Tabs.Trigger value="match">Match</Tabs.Trigger>
					{/if}
					{#if title.kind !== "extra"}
						<Tabs.Trigger value="artwork">Artwork</Tabs.Trigger>
					{/if}
				</Tabs.List>
				<Tabs.Content value="details" class="pt-4">
					<MetadataForm {title} />
				</Tabs.Content>
				{#if matched}
					<Tabs.Content value="match" class="grid gap-4 pt-4">
						<IdentifyPanel {title} {providers} />
					</Tabs.Content>
				{/if}
				{#if title.kind !== "extra"}
					<Tabs.Content value="artwork" class="pt-4">
						<ArtworkPicker {title} />
					</Tabs.Content>
				{/if}
			</Tabs.Root>
		{/if}
	</Dialog.Content>
</Dialog.Root>
