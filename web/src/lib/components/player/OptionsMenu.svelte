<script lang="ts">
import BookIcon from "@lucide/svelte/icons/book";
import ClipboardIcon from "@lucide/svelte/icons/clipboard";
import DiscIcon from "@lucide/svelte/icons/disc-3";
import DownloadIcon from "@lucide/svelte/icons/download";
import EllipsisIcon from "@lucide/svelte/icons/ellipsis";
import HeartIcon from "@lucide/svelte/icons/heart";
import InfoIcon from "@lucide/svelte/icons/info";
import ListIcon from "@lucide/svelte/icons/list";
import ListEndIcon from "@lucide/svelte/icons/list-end";
import ListPlusIcon from "@lucide/svelte/icons/list-plus";
import ListStartIcon from "@lucide/svelte/icons/list-start";
import MicVocalIcon from "@lucide/svelte/icons/mic-vocal";
import Share2Icon from "@lucide/svelte/icons/share-2";
import UserIcon from "@lucide/svelte/icons/user";
import { toast } from "svelte-sonner";
import { client } from "#lib/api/client.ts";
import type { components } from "#lib/api/schema.d.ts";
import IconButton from "#lib/components/IconButton.svelte";
import { Button } from "#lib/components/ui/button/index.ts";
import * as DropdownMenu from "#lib/components/ui/dropdown-menu/index.ts";
import { downloadFile } from "#lib/download.ts";
import { isFavourite, setFavourite } from "#lib/favourites.svelte.ts";
import { timecode } from "#lib/format.ts";
import { listening } from "#lib/player/listening.svelte.ts";
import { tracksOf } from "#lib/player/tracks.ts";
import { getShell } from "#lib/shell.svelte.ts";
import { goto } from "$app/navigation";
import CreditsDialog from "./CreditsDialog.svelte";
import SleepMenu from "./SleepMenu.svelte";
import SpeedMenu from "./SpeedMenu.svelte";

type Schemas = components["schemas"];

// What can be done with what plays, as Apple Music's Now Playing "⋯" offers:
// keep, favour and share it, queue its album, go to its album and artist,
// see its credits and lyrics, and stop on a timer; an audiobook's speed and
// chapters. Its title's page and lyrics are asked for as it is first opened.
let {
	size,
	onlyrics,
	onleave,
}: {
	size: "bar" | "large";
	// Shows the lyrics in Now Playing.
	onlyrics: () => void;
	// Before going to another page, as Now Playing closes.
	onleave?: () => void;
} = $props();

const shell = getShell();
const item = $derived(listening.current);

type Details = {
	id: string;
	page: Schemas["TitlePage"];
	lyrics: Schemas["Lyrics"] | null;
};
let details = $state<Details>();
const known = $derived(
	details && details.id === item?.id ? details : undefined,
);
const favourite = $derived(
	!!known && isFavourite(known.id, known.page.state?.favourite_at),
);
const lyrics = $derived(known?.lyrics?.lines ?? []);
// The version playing, saved whole where it is one file: an audiobook in
// several has no one file to give.
const version = $derived(
	known?.page.versions?.find((v) => v.id === item?.version) ??
		known?.page.versions?.[0],
);
const downloadable = $derived(version?.parts === 1);
let credits = $state(false);

async function load(id: string) {
	const [page, sung] = await Promise.all([
		client().GET("/api/v1/titles/{id}", { params: { path: { id } } }),
		client().GET("/api/v1/titles/{id}/lyrics", { params: { path: { id } } }),
	]);
	if (page.data && listening.current?.id === id) {
		details = { id, page: page.data, lyrics: sung.data ?? null };
	}
}

function opened(open: boolean) {
	if (open && item && details?.id !== item.id) void load(item.id);
}

// The track's page is kept here, not the route's, so what was asked is kept in
// it once saved.
function favour() {
	const was = known;
	if (!was) return;
	const on = !favourite;
	return setFavourite(was.id, on, () => {
		const state = {
			...was.page.state,
			favourite_at: on ? new Date().toISOString() : null,
		};
		if (details?.id === was.id)
			details = { ...details, page: { ...details.page, state } };
	});
}

function download() {
	if (item && version && downloadable)
		void downloadFile(item.id, version, item.title);
}

function share() {
	if (!item) return;
	const name = [item.title, item.by, item.from?.title]
		.filter(Boolean)
		.join(" · ");
	void shell.shareTitle(item.id, name);
}

async function queueAlbum(where: "next" | "last") {
	const album = item?.from;
	if (!album) return;
	const tracks = await tracksOf(album.id);
	if (where === "next") listening.playNext(tracks);
	else listening.addToQueue(tracks);
	toast.success(
		where === "next"
			? `${album.title} plays next.`
			: `${album.title} is queued.`,
	);
}

function go(id: string) {
	onleave?.();
	void goto(`/titles/${id}`);
}

async function copyLyrics() {
	await navigator.clipboard.writeText(lyrics.map((l) => l.text).join("\n"));
	toast.success("Lyrics copied.");
}
</script>

{#if item}
	<DropdownMenu.Root onOpenChange={opened}>
		<DropdownMenu.Trigger>
			{#snippet child({ props })}
				{#if size === "large"}
					<Button {...props} variant="ghost" aria-label="More options">
						<EllipsisIcon />
					</Button>
				{:else}
					<IconButton {...props} label="More options" icon={EllipsisIcon} />
				{/if}
			{/snippet}
		</DropdownMenu.Trigger>
		<DropdownMenu.Content align="end" class="w-64">
			<DropdownMenu.Group class="grid grid-cols-3 gap-1">
				{#each [
					{
						label: "Download",
						icon: DownloadIcon,
						act: download,
						pressed: undefined,
						ready: downloadable,
					},
					{
						label: favourite ? "Unfavourite" : "Favourite",
						icon: HeartIcon,
						act: favour,
						pressed: favourite,
						ready: !!known,
					},
					{
						label: "Share",
						icon: Share2Icon,
						act: share,
						pressed: undefined,
						ready: true,
					},
				] as action (action.act)}
					<DropdownMenu.Item
						disabled={!action.ready}
						aria-pressed={action.pressed}
						class="flex-col gap-1 py-2 text-xs"
						onSelect={action.act}
					>
						<action.icon class={action.pressed ? "fill-current" : ""} />
						{action.label}
					</DropdownMenu.Item>
				{/each}
			</DropdownMenu.Group>
			<DropdownMenu.Separator />
			<DropdownMenu.Group>
				{#if item.from}
					<DropdownMenu.Item onSelect={() => queueAlbum("next")}>
						<ListStartIcon />Play album next
					</DropdownMenu.Item>
					<DropdownMenu.Item onSelect={() => queueAlbum("last")}>
						<ListEndIcon />Add album to queue
					</DropdownMenu.Item>
				{/if}
				<DropdownMenu.Item
					onSelect={() => shell.pickPlaylist([item.id], item.title)}
				>
					<ListPlusIcon />Add to playlist…
				</DropdownMenu.Item>
				<DropdownMenu.Item
					onSelect={() =>
						shell.pickPlaylist(
							listening.queue.map((q) => q.id),
							"The queue",
						)}
				>
					<ListPlusIcon />Save queue as playlist…
				</DropdownMenu.Item>
			</DropdownMenu.Group>
			<DropdownMenu.Separator />
			<DropdownMenu.Group>
				{#if item.kind === "book"}
					<DropdownMenu.Item onSelect={() => go(item.id)}>
						<BookIcon />
						<span class="truncate">Go to book</span>
					</DropdownMenu.Item>
				{:else if item.from}
					{@const album = item.from}
					<DropdownMenu.Item onSelect={() => go(album.id)}>
						<DiscIcon />
						<span class="truncate">Go to {album.title}</span>
					</DropdownMenu.Item>
				{/if}
				{#if item.creator}
					{@const creator = item.creator}
					<DropdownMenu.Item onSelect={() => go(creator.id)}>
						<UserIcon />
						<span class="truncate">Go to {creator.title}</span>
					</DropdownMenu.Item>
				{/if}
				<DropdownMenu.Item disabled={!known} onSelect={() => (credits = true)}>
					<InfoIcon />View credits
				</DropdownMenu.Item>
				{#if lyrics.length}
					<DropdownMenu.Item onSelect={onlyrics}>
						<MicVocalIcon />Show lyrics
					</DropdownMenu.Item>
					<DropdownMenu.Item onSelect={copyLyrics}>
						<ClipboardIcon />Copy lyrics
					</DropdownMenu.Item>
				{/if}
			</DropdownMenu.Group>
			<DropdownMenu.Separator />
			{#if item.kind === "book"}
				<SpeedMenu />
				{#if item.chapters?.length}
					{@const chapters = item.chapters}
					<DropdownMenu.Sub>
						<DropdownMenu.SubTrigger
							><ListIcon />Chapters</DropdownMenu.SubTrigger
						>
						<DropdownMenu.SubContent class="max-h-80 overflow-y-auto">
							{#each chapters as c, i (c.start_ms)}
								<DropdownMenu.Item
									onSelect={() => listening.seek(c.start_ms / 1000)}
								>
									<span class="min-w-0 flex-1 truncate">
										{c.title || `Chapter ${i + 1}`}
									</span>
									<span class="text-ink-3 font-mono text-xs tabular-nums">
										{timecode(c.start_ms)}
									</span>
								</DropdownMenu.Item>
							{/each}
						</DropdownMenu.SubContent>
					</DropdownMenu.Sub>
				{/if}
			{/if}
			<SleepMenu />
		</DropdownMenu.Content>
	</DropdownMenu.Root>
	{#if known}
		<CreditsDialog
			bind:open={credits}
			{item}
			page={known.page}
			playback={listening.playback}
		/>
	{/if}
{/if}
