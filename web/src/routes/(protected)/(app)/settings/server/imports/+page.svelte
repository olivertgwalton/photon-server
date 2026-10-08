<script lang="ts">
import PageHeader from "#lib/components/PageHeader.svelte";
import { toast } from "svelte-sonner";
import { refreshAll } from "$app/navigation";
import { fields } from "#lib/form.js";
import { byName, importMisses, importSources, when } from "#lib/admin/words.js";
import { client } from "#lib/api/client.js";
import { problemMessage } from "#lib/api/problem.js";
import type { components } from "#lib/api/schema.js";
import Choice from "#lib/components/Choice.svelte";
import { Badge } from "#lib/components/ui/badge/index.js";
import { Button } from "#lib/components/ui/button/index.js";
import * as Dialog from "#lib/components/ui/dialog/index.js";
import * as Field from "#lib/components/ui/field/index.js";
import { Input } from "#lib/components/ui/input/index.js";

type Source = components["schemas"]["ImportSource"];

let { data } = $props();

const api = client();
let starting = $state(false);
let source = $state<Source>("plex");
let busy = $state(false);

const names = $derived(byName(data.profiles));
const sources = Object.entries(importSources).map(([value, label]) => ({
	value: value as Source,
	label,
}));
const profiles = $derived(
	data.profiles.map((p) => ({ value: p.id, label: p.name })),
);

// An import under way is read again every few seconds until it ends.
$effect(() => {
	if (
		!data.imports.some((i) => i.status === "queued" || i.status === "running")
	)
		return;
	const timer = setInterval(() => refreshAll(), 2000);
	return () => clearInterval(timer);
});

function dates(run: components["schemas"]["Import"]) {
	const started = `Started ${when.format(new Date(run.created_at))}`;
	return run.finished_at
		? `${started}, ended ${when.format(new Date(run.finished_at))}`
		: started;
}

async function start(event: SubmitEvent) {
	const form = fields(event);
	const text = (name: string) => String(form.get(name) ?? "");
	busy = true;
	const { error } = await api.POST("/api/v1/admin/imports", {
		body: {
			source,
			url: text("url"),
			profile_id: text("profile"),
			credentials:
				source === "plex"
					? { token: text("token") }
					: { username: text("username"), password: text("password") },
		},
	});
	busy = false;
	if (error) return toast.error(problemMessage(error));
	starting = false;
	await refreshAll();
}
</script>

<PageHeader
	title="Import watch history"
	description="Bring what a Plex, Jellyfin or Emby user has watched into a profile here: watched titles with their dates, plays, and where each was stopped. Titles are matched by their TMDB, TheTVDB or IMDb ids, and nothing newer here is overwritten."
>
	{#snippet actions()}
		<Dialog.Root bind:open={starting}>
			<Dialog.Trigger>
				{#snippet child({
					props,
				})}
					<Button {...props}>Import</Button>
				{/snippet}
			</Dialog.Trigger>
			<Dialog.Content class="sm:max-w-lg">
				<form onsubmit={start} class="grid gap-6">
					<Dialog.Header>
						<Dialog.Title>Import watch history</Dialog.Title>
						<Dialog.Description>
							The server is signed in to now, so a wrong address or credentials
							are caught at once. They are kept only until the import ends.
						</Dialog.Description>
					</Dialog.Header>
					<Field.Group>
						<Field.Field>
							<Field.Label for="import-source">From</Field.Label>
							<Choice
								id="import-source"
								name="source"
								options={sources}
								bind:value={source}
							/>
						</Field.Field>
						<Field.Field>
							<Field.Label for="import-url">Address</Field.Label>
							<Input
								id="import-url"
								name="url"
								type="url"
								required
								placeholder={source === "plex"
									? "http://192.168.1.10:32400"
									: "http://192.168.1.10:8096"}
							/>
						</Field.Field>
						{#if source === "plex"}
							<Field.Field>
								<Field.Label for="import-token">Token</Field.Label>
								<Input
									id="import-token"
									name="token"
									type="password"
									required
									autocomplete="off"
								/>
								<Field.Description>
									The X-Plex-Token of the account whose history is imported.
								</Field.Description>
							</Field.Field>
						{:else}
							<Field.Field>
								<Field.Label for="import-username">Username</Field.Label>
								<Input
									id="import-username"
									name="username"
									required
									autocomplete="off"
								/>
							</Field.Field>
							<Field.Field>
								<Field.Label for="import-password">Password</Field.Label>
								<Input
									id="import-password"
									name="password"
									type="password"
									autocomplete="off"
								/>
							</Field.Field>
						{/if}
						<Field.Field>
							<Field.Label for="import-profile">Into</Field.Label>
							<Choice
								id="import-profile"
								name="profile"
								options={profiles}
								value={data.profiles[0]?.id}
							/>
						</Field.Field>
					</Field.Group>
					<Dialog.Footer>
						<Button type="submit" disabled={busy}>Start import</Button>
					</Dialog.Footer>
				</form>
			</Dialog.Content>
		</Dialog.Root>
	{/snippet}
</PageHeader>

{#if data.imports.length}
	<ul class="grid gap-3">
		{#each data.imports as run (run.id)}
			<li class="bg-raise grid gap-3 rounded-xl p-4">
				<div class="flex flex-wrap items-center gap-2">
					<Badge variant="outline">{importSources[run.source]}</Badge>
					<p class="text-ink min-w-0 truncate font-mono text-sm">{run.url}</p>
					<p class="text-ink-3 text-sm">
						into {names.get(run.profile_id) ?? "a removed profile"}
					</p>
				</div>
				{#if run.status === "queued" || run.status === "running"}
					<p class="text-ink-2 text-sm">
						{run.status === "queued" ? "Waiting to start…" : "Importing…"}
					</p>
				{:else if run.status === "failed"}
					<p class="text-destructive text-sm">Failed: {run.error}</p>
				{:else}
					<p class="text-ink-2 text-sm">
						{run.matched}
						matched: {run.imported} imported, {run.skipped} left as they are
						here. {run.unmatched} not found here.
					</p>
				{/if}
				{#if run.misses.length}
					<details class="text-sm">
						<summary class="text-ink-2 cursor-pointer">
							Titles not imported
						</summary>
						<ul class="mt-2 grid gap-1">
							{#each run.misses as miss, i (i)}
								<li class="flex justify-between gap-4">
									<span class="text-ink truncate">{miss.title}</span>
									<span class="text-ink-3 shrink-0"
										>{importMisses[miss.reason]}</span
									>
								</li>
							{/each}
						</ul>
					</details>
				{/if}
				<p class="text-ink-3 text-xs">
					{dates(run)}
				</p>
			</li>
		{/each}
	</ul>
{:else}
	<p class="text-ink-3 text-sm">No imports yet.</p>
{/if}
