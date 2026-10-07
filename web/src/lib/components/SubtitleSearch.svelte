<script lang="ts">
import StarIcon from "@lucide/svelte/icons/star";
import { act } from "#lib/act.js";
import { subtitleSearch } from "#lib/actions.svelte.js";
import { client } from "#lib/api/client.js";
import { problemMessage } from "#lib/api/problem.js";
import type { components } from "#lib/api/schema.js";
import { Button } from "#lib/components/ui/button/index.js";
import * as Dialog from "#lib/components/ui/dialog/index.js";
import * as Select from "#lib/components/ui/select/index.js";
import { language, subtitleLanguages } from "#lib/player/words.js";

type Found = components["schemas"]["FoundSubtitles"];

// Finds subtitles for a film or an episode on the providers an admin set up,
// in a language, and keeps the one chosen beside its copy for everyone, as
// Plex's and Jellyfin's subtitle search do. One made for the very file, found
// by its hash, is starred and comes first.

let lang = $state("");
let found = $state<Found>();
let problem = $state("");
let searching = $state(false);

const languages = $derived(
	[...new Set([lang, ...subtitleLanguages])]
		.filter(Boolean)
		.map((tag) => ({ value: tag, label: language(tag) }))
		.sort((a, b) => a.label.localeCompare(b.label)),
);

// Opened, it searches at once in the reader's subtitle language, else their
// browser's.
$effect(() => {
	if (!subtitleSearch.open) return;
	found = undefined;
	client()
		.GET("/api/v1/me/preferences")
		.then(({ data }) => {
			lang = data?.subtitle_language || navigator.language.split("-")[0];
			search();
		});
});

async function search() {
	searching = true;
	problem = "";
	const { data, error } = await client().GET(
		"/api/v1/titles/{id}/subtitles/search",
		{ params: { path: { id: subtitleSearch.id }, query: { language: lang } } },
	);
	searching = false;
	found = data;
	if (error) problem = problemMessage(error);
}

async function fetchOne(s: Found["items"][number]) {
	if (!found) return;
	const done = await act(
		client().POST("/api/v1/titles/{id}/subtitles", {
			params: { path: { id: subtitleSearch.id } },
			body: { version_id: found.version_id, ...s },
		}),
		`${language(s.language) || "Subtitles"} added to ${subtitleSearch.title}.`,
	);
	if (done) subtitleSearch.open = false;
}
</script>

<Dialog.Root bind:open={subtitleSearch.open}>
	<Dialog.Content class="max-h-[85vh] grid-rows-[auto_auto_1fr]">
		<Dialog.Header>
			<Dialog.Title>Find subtitles</Dialog.Title>
			<Dialog.Description>{subtitleSearch.title}</Dialog.Description>
		</Dialog.Header>
		<Select.Root
			type="single"
			value={lang}
			onValueChange={(v) => {
				lang = v;
				search();
			}}
		>
			<Select.Trigger aria-label="Language" class="w-56">
				{language(lang) || "Language"}
			</Select.Trigger>
			<Select.Content>
				{#each languages as l (l.value)}
					<Select.Item value={l.value} label={l.label} />
				{/each}
			</Select.Content>
		</Select.Root>
		<div class="min-h-0 overflow-y-auto" aria-busy={searching}>
			{#if problem}
				<p class="text-ink-3 text-sm" role="alert">{problem}</p>
			{:else if searching}
				<p class="text-ink-3 text-sm">Searching…</p>
			{:else if found && !found.items.length}
				<p class="text-ink-3 text-sm">None found in {language(lang)}.</p>
			{:else if found}
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
										s.downloads && `${s.downloads.toLocaleString()} downloads`,
									]
										.filter(Boolean)
										.join(" · ")}
								</p>
							</div>
							<Button size="sm" variant="outline" onclick={() => fetchOne(s)}>
								Add
							</Button>
						</li>
					{/each}
				</ul>
			{/if}
		</div>
	</Dialog.Content>
</Dialog.Root>
