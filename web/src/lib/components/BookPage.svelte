<script lang="ts">
import BookOpenIcon from "@lucide/svelte/icons/book-open";
import DownloadIcon from "@lucide/svelte/icons/download";
import HeadphonesIcon from "@lucide/svelte/icons/headphones";
import HeartIcon from "@lucide/svelte/icons/heart";
import { toast } from "svelte-sonner";
import { setMark } from "#lib/actions.ts";
import { client } from "#lib/api/client.ts";
import { problemMessage } from "#lib/api/problem.ts";
import type { components } from "#lib/api/schema.d.ts";
import {
	bookListen,
	chaptersOf,
	ebookProfile,
	formatNames,
	formatOf,
	seriesLine,
} from "#lib/books.ts";
import CoverHero from "#lib/components/CoverHero.svelte";
import { Button } from "#lib/components/ui/button/index.ts";
import * as Item from "#lib/components/ui/item/index.ts";
import * as Table from "#lib/components/ui/table/index.ts";
import { bytes, resumeLabel, runtime, timecode } from "#lib/format.ts";
import { listening } from "#lib/player/listening.svelte.ts";

type Schemas = components["schemas"];
type Version = Schemas["VersionPage"];

// A book: its cover, author and series, and each of its formats, an
// audiobook listened to here and an ebook downloaded to read elsewhere, as
// Audiobookshelf's book page has them.
let { title: t }: { title: Schemas["TitlePage"] } = $props();

const versions = $derived(
	(t.versions ?? []).map((v) => ({ version: v, format: formatOf(v) })),
);
const audiobook = $derived(
	versions.find((v) => v.format === "audiobook")?.version,
);
const ebook = $derived(versions.find((v) => v.format !== "audiobook")?.version);
const chapters = $derived(audiobook ? chaptersOf(audiobook) : []);
const position = $derived(t.state?.position_ms ?? 0);
const favourite = $derived(!!t.state?.favourite_at);

function listen(version: Version, from?: number) {
	listening.play([bookListen(t, version, from)]);
}

// An ebook's file comes from a playback of it, as every other file does; it
// is a download its address alone opens, which the browser saves.
let fetching = $state<string>();
async function read(version: Version) {
	fetching = version.id;
	const { data, error } = await client().POST("/api/v1/titles/{id}/play", {
		params: { path: { id: t.id } },
		body: { version_id: version.id, profile: ebookProfile(version) },
	});
	fetching = undefined;
	const url = data?.parts?.[0]?.url;
	if (!url) {
		toast.error(problemMessage(error));
		return;
	}
	const link = document.createElement("a");
	link.href = url;
	link.download = "";
	link.click();
}
</script>

<article class="grid gap-10">
	<CoverHero
		title={t}
		shape="poster"
		by={t.author}
		facts={[
			seriesLine(t.series, t.series_index),
			t.year,
			audiobook && runtime(audiobook.duration_ms),
		]}
	>
		{#snippet actions()}
			{#if audiobook}
				{@const version = audiobook}
				<Button
					size="lg"
					class="rounded-full px-5"
					onclick={() => listen(version)}
				>
					<HeadphonesIcon />
					{position ? resumeLabel(position) : "Listen"}
				</Button>
				{#if position}
					<Button variant="glass" size="lg" onclick={() => listen(version, 0)}>
						From the beginning
					</Button>
				{/if}
			{/if}
			{#if ebook}
				{@const version = ebook}
				<Button
					variant={audiobook ? "glass" : "default"}
					size="lg"
					class="rounded-full px-5"
					disabled={fetching === version.id}
					aria-busy={fetching === version.id}
					onclick={() => read(version)}
				>
					<BookOpenIcon />Read
				</Button>
			{/if}
			<Button
				variant="glass"
				size="icon-lg"
				aria-pressed={favourite}
				aria-label="Favourite"
				title={favourite ? "Remove from favourites" : "Add to favourites"}
				onclick={() => setMark(t.id, "favourite", !favourite)}
			>
				<HeartIcon class={favourite ? "fill-current" : "text-ink-3"} />
			</Button>
		{/snippet}
	</CoverHero>

	{#if versions.length}
		<section aria-labelledby="formats" class="max-w-3xl">
			<h2 id="formats" class="heading mb-3">Formats</h2>
			<Item.Group class="gap-2">
				{#each versions as { version, format } (version.id)}
					<Item.Root variant="outline">
						<Item.Media variant="icon">
							{#if format === "audiobook"}
								<HeadphonesIcon />
							{:else}
								<BookOpenIcon />
							{/if}
						</Item.Media>
						<Item.Content>
							<Item.Title>
								{version.display_title || formatNames[format]}
							</Item.Title>
							<Item.Description>
								{[
									format === "audiobook" && runtime(version.duration_ms),
									format === "audiobook" &&
										version.parts > 1 &&
										`${version.parts} files`,
									bytes(version.size_bytes),
								]
									.filter(Boolean)
									.join(" · ")}
							</Item.Description>
						</Item.Content>
						<Item.Actions>
							{#if format === "audiobook"}
								<Button
									variant="outline"
									size="sm"
									onclick={() => listen(version)}
								>
									<HeadphonesIcon />Listen
								</Button>
							{:else}
								<Button
									variant="outline"
									size="sm"
									disabled={fetching === version.id}
									onclick={() => read(version)}
								>
									<DownloadIcon />Download
								</Button>
							{/if}
						</Item.Actions>
					</Item.Root>
				{/each}
			</Item.Group>
		</section>
	{/if}

	{#if audiobook && chapters.length}
		{@const version = audiobook}
		<section aria-labelledby="chapters" class="max-w-3xl">
			<h2 id="chapters" class="heading mb-3">Chapters</h2>
			<Table.Root class="table-fixed">
				<colgroup>
					<col class="w-12">
					<col>
					<col class="w-24">
				</colgroup>
				<Table.Header class="sr-only">
					<Table.Row>
						<Table.Head>Number</Table.Head>
						<Table.Head>Chapter</Table.Head>
						<Table.Head>Starts at</Table.Head>
					</Table.Row>
				</Table.Header>
				<Table.Body>
					{#each chapters as chapter, i (chapter.start_ms)}
						<Table.Row class="border-line/50 relative">
							<Table.Cell class="text-ink-3 text-right font-mono tabular-nums">
								{i + 1}
							</Table.Cell>
							<Table.Cell>
								<button
									type="button"
									class="text-ink block w-full truncate text-left outline-none after:absolute after:inset-0 focus-visible:underline"
									onclick={() => listen(version, chapter.start_ms)}
								>
									{chapter.title || `Chapter ${i + 1}`}
								</button>
							</Table.Cell>
							<Table.Cell class="text-ink-3 text-right font-mono tabular-nums">
								{timecode(chapter.start_ms)}
							</Table.Cell>
						</Table.Row>
					{/each}
				</Table.Body>
			</Table.Root>
		</section>
	{/if}
</article>
