<script lang="ts">
import { type EditorTab, editor } from "#lib/actions.svelte.js";
import { client } from "#lib/api/client.js";
import { problemMessage } from "#lib/api/problem.js";
import ArtworkPicker from "#lib/components/admin/ArtworkPicker.svelte";
import IdentifyPanel from "#lib/components/admin/IdentifyPanel.svelte";
import MetadataForm from "#lib/components/admin/MetadataForm.svelte";
import TitleLocale from "#lib/components/admin/TitleLocale.svelte";
import * as Dialog from "#lib/components/ui/dialog/index.js";
import * as Tabs from "#lib/components/ui/tabs/index.js";

// A title's editing over the page it was opened from, as Plex's Edit dialog
// is: its details, its match and its pictures, each the admin title page's.

// Asked each time it opens, so what was edited since is there.
function load() {
	const api = client();
	return Promise.all([
		api.GET("/api/v1/titles/{id}", { params: { path: { id: editor.id } } }),
		api.GET("/api/v1/admin/providers"),
		api.GET("/api/v1/admin/locales"),
	]);
}

const part = "-mx-1 min-h-0 flex-1 overflow-y-auto px-1 pt-4";
</script>

<Dialog.Root bind:open={editor.open}>
	<!-- One height whichever part is shown, as Plex's Edit dialog keeps: the
		tabs stay put and the part beneath them scrolls. -->
	<Dialog.Content
		class="flex h-[min(85svh,48rem)] flex-col overflow-hidden sm:max-w-3xl"
	>
		{#await load()}
			<Dialog.Header>
				<Dialog.Title>Edit</Dialog.Title>
			</Dialog.Header>
			<p class="text-ink-3">Loading…</p>
		{:then [{ data: title, error }, providers, locales]}
			<Dialog.Header>
				<Dialog.Title>Edit {title?.title ?? ""}</Dialog.Title>
			</Dialog.Header>
			{#if !title}
				<p role="alert" class="text-destructive">{problemMessage(error)}</p>
			{:else}
				{@const matched = title.kind === "movie" || title.kind === "show"}
				<Tabs.Root
					value={editor.tab}
					onValueChange={(tab) => (editor.tab = tab as EditorTab)}
					class="min-h-0 flex-1"
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
					<Tabs.Content value="details" class="{part} grid content-start gap-8">
						<MetadataForm {title} />
						{#if matched && locales.data}
							<TitleLocale {title} locales={locales.data} />
						{/if}
					</Tabs.Content>
					{#if matched}
						<Tabs.Content value="match" class="{part} grid content-start gap-4">
							<IdentifyPanel {title} providers={providers.data?.items ?? []} />
						</Tabs.Content>
					{/if}
					{#if title.kind !== "extra"}
						<Tabs.Content value="artwork" class={part}>
							<ArtworkPicker {title} />
						</Tabs.Content>
					{/if}
				</Tabs.Root>
			{/if}
		{/await}
	</Dialog.Content>
</Dialog.Root>
