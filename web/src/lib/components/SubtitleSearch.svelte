<script lang="ts">
import StarIcon from "@lucide/svelte/icons/star";
import { act } from "#lib/act.js";
import { client } from "#lib/api/client.js";
import { problemMessage } from "#lib/api/problem.js";
import type { components } from "#lib/api/schema.js";
import Choice from "#lib/components/Choice.svelte";
import { Button } from "#lib/components/ui/button/index.js";
import * as Dialog from "#lib/components/ui/dialog/index.js";
import { language, localeOptions } from "#lib/format.js";
import { subtitleLanguages } from "#lib/player/words.js";
import { getShell } from "#lib/shell.svelte.js";

const shell = getShell();

type Found = components["schemas"]["FoundSubtitles"];

// Finds subtitles for a film or an episode on the providers an admin set up,
// in a language, and keeps the one chosen beside its copy for everyone, as
// Plex's and Jellyfin's subtitle search do. One made for the very file, found
// by its hash, is starred and comes first.

// Opened, it searches at once in the reader's subtitle language, else their
// browser's.
async function startLanguage(): Promise<string> {
	const { data } = await client().GET("/api/v1/profile/preferences");
	return data?.subtitle_language || navigator.language.split("-")[0];
}

const options = (lang: string) =>
	localeOptions(new Set([lang, ...subtitleLanguages].filter(Boolean)));

function search(lang: string) {
	return client().GET("/api/v1/titles/{id}/subtitles/candidates", {
		params: {
			path: { id: shell.subtitleSearch.id },
			query: { language: lang },
		},
	});
}

async function fetchOne(found: Found, s: Found["items"][number]) {
	const done = await act(
		client().POST("/api/v1/titles/{id}/subtitles", {
			params: { path: { id: shell.subtitleSearch.id } },
			body: { version_id: found.version_id, ...s },
		}),
		`${language(s.language) || "Subtitles"} added to ${shell.subtitleSearch.title}.`,
	);
	if (done) shell.subtitleSearch.open = false;
}
</script>

<Dialog.Root bind:open={shell.subtitleSearch.open}>
	<Dialog.Content class="max-h-[85vh] grid-rows-[auto_auto_1fr]">
		<Dialog.Header>
			<Dialog.Title>Find subtitles</Dialog.Title>
			<Dialog.Description>{shell.subtitleSearch.title}</Dialog.Description>
		</Dialog.Header>
		<svelte:boundary>
			{@const first = await startLanguage()}
			{@const lang = shell.subtitleSearch.language || first}
			<Choice
				aria-label="Language"
				class="w-56"
				value={lang}
				options={options(lang)}
				onchange={(v: string) => {
					shell.subtitleSearch.language = v;
				}}
			/>
			<svelte:boundary>
				{@const { data: found, error } = await search(lang)}
				<!-- Another language's results stay, dimmed, until its own come. -->
				<div
					class={[
						"min-h-0 overflow-y-auto transition-opacity",
						$effect.pending() && "opacity-60",
					]}
					aria-busy={$effect.pending() > 0}
				>
					{#if !found}
						<p class="text-ink-3 text-sm" role="alert">
							{problemMessage(error)}
						</p>
					{:else if !found.items.length}
						<p class="text-ink-3 text-sm">None found in {language(lang)}.</p>
					{:else}
						<ul class="grid gap-1">
							{#each found.items as s (`${s.source}/${s.id}`)}
								<li
									class="hover:bg-accent flex items-center gap-3 rounded-md px-3 py-2"
								>
									<div class="min-w-0 flex-1">
										<p class="text-ink flex items-center gap-1 font-semibold">
											{#if s.for_release}
												<StarIcon
													class="size-4 shrink-0 fill-current"
													aria-label="Made for this file"
												/>
											{/if}
											<span class="truncate"
												>{s.release || language(s.language)}</span
											>
										</p>
										<p class="text-ink-3 text-sm">
											{[
												language(s.language),
												s.hearing_impaired && "SDH",
												s.forced && "Forced",
												s.downloads &&
													`${s.downloads.toLocaleString()} downloads`,
											]
												.filter(Boolean)
												.join(" · ")}
										</p>
									</div>
									<Button
										size="sm"
										variant="outline"
										onclick={() => fetchOne(found, s)}
									>
										Add
									</Button>
								</li>
							{/each}
						</ul>
					{/if}
				</div>
				{#snippet pending()}
					<div class="min-h-0 overflow-y-auto" aria-busy="true">
						<p class="text-ink-3 text-sm">Searching…</p>
					</div>
				{/snippet}
			</svelte:boundary>
			{#snippet pending()}
				<p class="text-ink-3 text-sm">Searching…</p>
			{/snippet}
		</svelte:boundary>
	</Dialog.Content>
</Dialog.Root>
