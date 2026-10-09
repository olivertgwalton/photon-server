<script lang="ts">
import Artwork from "#lib/components/Artwork.svelte";
import BookmarkIcon from "@lucide/svelte/icons/bookmark";
import CaptionsIcon from "@lucide/svelte/icons/captions";
import CheckIcon from "@lucide/svelte/icons/check";
import DownloadIcon from "@lucide/svelte/icons/download";
import EllipsisIcon from "@lucide/svelte/icons/ellipsis";
import ExternalLinkIcon from "@lucide/svelte/icons/external-link";
import FilmIcon from "@lucide/svelte/icons/film";
import HeartIcon from "@lucide/svelte/icons/heart";
import InfoIcon from "@lucide/svelte/icons/info";
import ListPlusIcon from "@lucide/svelte/icons/list-plus";
import PlayIcon from "@lucide/svelte/icons/play";
import RotateCcwIcon from "@lucide/svelte/icons/rotate-ccw";
import WrenchIcon from "@lucide/svelte/icons/wrench";
import { goto } from "$app/navigation";
import { findSubtitles, pickPlaylist, setMark } from "#lib/actions.svelte.js";
import { type Extra, extrasOf } from "#lib/extras.js";
import { blurStyle, isDark } from "#lib/blurhash.js";
import CardGrid from "#lib/components/CardGrid.svelte";
import Prose from "#lib/components/Prose.svelte";
import DownloadDialog from "#lib/components/DownloadDialog.svelte";
import MediaInfo from "#lib/components/MediaInfo.svelte";
import ExtraCard from "#lib/components/ExtraCard.svelte";
import PersonCard from "#lib/components/PersonCard.svelte";
import PlayChoices from "#lib/components/PlayChoices.svelte";
import RatingScore from "#lib/components/RatingScore.svelte";
import Rail from "#lib/components/Rail.svelte";
import Choice from "#lib/components/Choice.svelte";
import { client } from "#lib/api/client.js";
import { episodesOf } from "#lib/seasons.js";
import ThemeTune from "#lib/components/ThemeTune.svelte";
import { Button } from "#lib/components/ui/button/index.js";
import * as DropdownMenu from "#lib/components/ui/dropdown-menu/index.js";
import {
	count,
	timecode,
	episodeLabel,
	titleWithShow,
	playHref,
	onDisk,
	runtime,
} from "#lib/format.js";

let { data } = $props();

const t = $derived(data.title);
const art = (kind: string) => t.artwork?.[kind]?.[0];
const playable = $derived(
	t.kind === "movie" || t.kind === "episode" || t.kind === "extra",
);
const admin = $derived(data.me.role === "admin");

let version = $state<string>();
let audio = $state<number>();
let subtitle = $state<number | "off">();
let mediaInfo = $state(false);
let download = $state(false);

const chosen = $derived(onDisk(t.versions, version));
const duration = $derived(chosen?.duration_ms);
const position = $derived(t.state?.position_ms ?? 0);
const choice = $derived({
	version: (t.versions?.length ?? 0) > 1 ? chosen?.id : undefined,
	audio,
	subtitle,
});
const watched = $derived(!!t.state?.watched_at);
const favourite = $derived(!!t.state?.favourite_at);
const watchlisted = $derived(!!t.state?.watchlisted_at);

const now = new Date();
const endsAt = $derived(
	duration && playable
		? new Date(now.getTime() + duration - position).toLocaleTimeString(
				undefined,
				{ hour: "numeric", minute: "2-digit" },
			)
		: undefined,
);

const facts = $derived(
	[
		t.kind === "episode" &&
			episodeLabel(t.season_number, t.episode_number, t.episode_end),
		t.year,
		t.certificate,
		duration && runtime(duration),
	].filter(Boolean),
);

// The season the row shows, where the reader picked another than the page's:
// kept with the page it was picked on, so the next page shows its own.
let chosenSeason = $state<{
	on: string;
	season: { id: string; title: string; number: number };
	episodes: NonNullable<typeof data.seasonEpisodes>;
}>();
const picked = $derived(
	chosenSeason?.on === t.id ? chosenSeason.season : undefined,
);
const episodes = $derived(
	chosenSeason?.on === t.id ? chosenSeason.episodes : data.seasonEpisodes,
);
async function pick(id: string) {
	const season = data.seasons?.find((s) => s.id === id);
	if (!season) return;
	const on = t.id;
	const shown =
		id === t.season?.id ? data.seasonEpisodes : await episodesOf(client(), id);
	chosenSeason = { on, season, episodes: shown ?? [] };
}

const credits = $derived(data.cast);
const directors = $derived(
	credits.filter((c) =>
		c.kinds.some((k) => k === "director" || k === "creator"),
	),
);
const writers = $derived(credits.filter((c) => c.kinds.includes("writer")));

const trailer = $derived(t.extras?.find((e) => e.extra_kind === "trailer"));

// Whose box sets, links and likes the page shows: an episode's are its
// show's, which has no page of its own.
const about = $derived(data.show ?? t);
const extras = $derived(
	data.extras.length || !data.show
		? { of: t.id, items: data.extras }
		: { of: data.show.id, items: extrasOf(data.show) },
);
const collections = $derived(
	(about.collections ?? []).map((c) => ({ ...c, kind: "collection" as const })),
);

const links = $derived.by(() => {
	const ids = about.ids ?? {};
	const out: { name: string; href: string }[] = [];
	if (about.kind !== "movie" && about.kind !== "show") return out;
	if (ids.imdb) {
		out.push({ name: "IMDb", href: `https://www.imdb.com/title/${ids.imdb}/` });
	}
	if (ids.tmdb) {
		out.push({
			name: "TMDB",
			href: `https://www.themoviedb.org/${about.kind === "show" ? "tv" : "movie"}/${ids.tmdb}`,
		});
	}
	if (ids.tvdb && about.kind === "show") {
		out.push({
			name: "TheTVDB",
			href: `https://thetvdb.com/dereferrer/series/${ids.tvdb}`,
		});
	}
	return out;
});

const adminTools = $derived(
	[
		["edit", "Edit metadata"],
		["identify", "Identify"],
		["refresh", "Refresh metadata"],
		["artwork", "Edit artwork"],
		playable && t.kind !== "extra" && ["markers", "Edit markers"],
	].filter((tool) => !!tool),
);

// What sits beside Play: panes of glass over the backdrop, Play the one
// filled control on the page.
const glass =
	"rounded-full border-white/10 bg-white/8 backdrop-blur-xl hover:bg-white/14";

const backdrop = $derived(art("backdrop"));
const logo = $derived(art("logo"));
const darkLogo = $derived(logo ? isDark(t.blurhashes?.[logo]) : false);
const poster = $derived(art("poster"));
</script>

<svelte:head>
	<title>{titleWithShow(t)} · Photon</title>
</svelte:head>

<article class="grid gap-10">
	<header class="relative -mx-3 -mt-6 sm:-mx-6">
		{#if backdrop}
			<div
				class="absolute inset-x-0 top-0 h-[70svh] overflow-hidden"
				style={blurStyle(t.blurhashes?.[backdrop])}
			>
				<!-- A new element for each title, so the old picture goes at once and the
					new one fades in over its blur. -->
				{#key backdrop}
					<Artwork
						id={backdrop}
						shape="backdrop"
						loading="eager"
						sizes="100vw"
						fetchpriority="high"
						class="size-full object-cover object-top transition-opacity duration-700 data-loading:opacity-0"
					/>
				{/key}
				<div
					class="from-ground via-ground/70 absolute inset-0 bg-linear-to-t to-transparent"
				></div>
				<div
					class="from-ground/90 absolute inset-0 bg-linear-to-r via-transparent to-transparent"
				></div>
			</div>
		{/if}
		<div
			class={[
				"relative grid gap-5 px-3 sm:px-6",
				backdrop ? "pt-[28svh]" : "pt-6",
			]}
		>
			<div class="flex items-end gap-6">
				{#if poster && !backdrop}
					<Artwork
						id={poster}
						shape="poster"
						loading="eager"
						sizes="12rem"
						class="hidden aspect-[2/3] w-48 rounded-lg object-cover sm:block"
						style={blurStyle(t.blurhashes?.[poster])}
					/>
				{/if}
				<h1 class="max-w-3xl">
					{#if logo}
						{#key logo}
							<Artwork
								id={logo}
								shape="still"
								loading="eager"
								sizes="24rem"
								alt={t.title}
								class="max-h-36 w-auto max-w-[min(24rem,80vw)] object-contain object-left transition-opacity duration-500 data-loading:opacity-0 {darkLogo
									? "lettering-glow"
									: ""}"
							/>
						{/key}
					{:else}
						<span class="title block text-3xl md:text-5xl">{t.title}</span>
					{/if}
				</h1>
			</div>
			{#if facts.length || endsAt}
				<p class="text-ink-2 flex flex-wrap gap-x-3 gap-y-1 text-sm">
					{#each facts as fact, i (i)}
						<span>{fact}</span>
					{/each}
					{#if endsAt}
						<span>Ends at {endsAt}</span>
					{/if}
				</p>
			{/if}
			{#if t.genres?.length}
				<p class="text-ink-2 text-sm">{t.genres.join(", ")}</p>
			{/if}
			{#if t.ratings?.length}
				<ul class="flex flex-wrap gap-2" aria-label="Ratings">
					{#each t.ratings as rating, i (`${rating.site}-${i}`)}
						<li
							class="rounded-full bg-white/8 px-3 py-1 text-sm backdrop-blur-xl"
						>
							<RatingScore {rating} />
						</li>
					{/each}
				</ul>
			{/if}
			{#if t.tagline}
				<p class="text-ink font-heading text-lg italic">{t.tagline}</p>
			{/if}
			{#if t.overview}
				<Prose
					text={t.overview}
					lines={3}
					title={titleWithShow(t)}
					class="text-ink-2 max-w-2xl"
				/>
			{/if}

			<div class="flex flex-wrap items-center gap-2">
				{#if playable && chosen}
					<Button
						href={playHref(t.id, choice)}
						size="lg"
						class="rounded-full px-5"
					>
						<PlayIcon class="fill-current" />
						{position ? `Resume from ${timecode(position)}` : "Play"}
					</Button>
					{#if position}
						<Button
							href={playHref(t.id, { ...choice, t: 0 })}
							variant="ghost"
							class={glass}
							size="lg"
						>
							<RotateCcwIcon />From the beginning
						</Button>
					{/if}
				{:else if t.fetching}
					<!-- A remote title whose provider is fetching its copy plays once it has it. -->
					<p class="text-ink-2 text-sm">
						Being fetched. It plays once its provider has it; look again in a
						few minutes.
					</p>
				{/if}
				{#if trailer}
					<Button
						href={playHref(trailer.id)}
						variant="ghost"
						class={glass}
						size="lg"
					>
						<FilmIcon />Trailer
					</Button>
				{/if}
				{#if t.kind !== "collection"}
					<Button
						variant="ghost"
						class={glass}
						size="icon-lg"
						aria-pressed={watched}
						aria-label="Watched"
						title={watched ? "Mark as unwatched" : "Mark as watched"}
						onclick={() => setMark(t.id, "watched", !watched)}
					>
						<CheckIcon class={watched ? "stroke-[3]" : "text-ink-3"} />
					</Button>
				{/if}
				<Button
					variant="ghost"
					class={glass}
					size="icon-lg"
					aria-pressed={favourite}
					aria-label="Favourite"
					title={favourite ? "Remove from favourites" : "Add to favourites"}
					onclick={() => setMark(t.id, "favourite", !favourite)}
				>
					<HeartIcon class={favourite ? "fill-current" : "text-ink-3"} />
				</Button>
				{#if t.kind !== "collection"}
					<Button
						variant="ghost"
						class={glass}
						size="icon-lg"
						aria-pressed={watchlisted}
						aria-label="Watchlist"
						title={watchlisted ? "Remove from watchlist" : "Add to watchlist"}
						onclick={() => setMark(t.id, "watchlist", !watchlisted)}
					>
						<BookmarkIcon class={watchlisted ? "fill-current" : "text-ink-3"} />
					</Button>
				{/if}
				<DropdownMenu.Root>
					<DropdownMenu.Trigger>
						{#snippet child({
							props,
						})}
							<Button
								variant="ghost"
								class={glass}
								size="icon-lg"
								aria-label="More"
								{...props}
							>
								<EllipsisIcon />
							</Button>
						{/snippet}
					</DropdownMenu.Trigger>
					<DropdownMenu.Content align="start" class="w-56">
						{#if t.kind !== "extra"}
							<DropdownMenu.Item
								onSelect={() => pickPlaylist([t.id], titleWithShow(t))}
							>
								<ListPlusIcon />Add to playlist…
							</DropdownMenu.Item>
						{/if}
						{#if playable && chosen}
							<DropdownMenu.Item onSelect={() => (download = true)}>
								<DownloadIcon />Download…
							</DropdownMenu.Item>
						{/if}
						{#if chosen && (t.kind === "movie" || t.kind === "episode")}
							<DropdownMenu.Item
								onSelect={() => findSubtitles(t.id, titleWithShow(t))}
							>
								<CaptionsIcon />Find subtitles…
							</DropdownMenu.Item>
						{/if}
						{#if t.versions?.length}
							<DropdownMenu.Item onSelect={() => (mediaInfo = true)}>
								<InfoIcon />Media info
							</DropdownMenu.Item>
						{/if}
						{#if admin}
							<DropdownMenu.Separator />
							<DropdownMenu.Group>
								<DropdownMenu.GroupHeading>Admin</DropdownMenu.GroupHeading>
								{#each adminTools as [tool, name] (tool)}
									<DropdownMenu.Item
										onSelect={() =>
											goto(`/settings/server/titles/${t.id}#${tool}`)}
									>
										<WrenchIcon />{name}
									</DropdownMenu.Item>
								{/each}
								{#if t.show}
									<!-- A show has no page of its own to manage it from. -->
									<DropdownMenu.Item
										onSelect={() =>
											goto(`/settings/server/titles/${t.show?.id}`)}
									>
										<WrenchIcon />Manage show
									</DropdownMenu.Item>
								{/if}
							</DropdownMenu.Group>
						{/if}
					</DropdownMenu.Content>
				</DropdownMenu.Root>
				{#if data.themeMusic && t.themes?.length}
					<!-- An episode of the same show goes on with the tune already playing. -->
					{#key t.themes.join()}
						<ThemeTune themes={t.themes} />
					{/key}
				{/if}
			</div>

			{#if playable && t.versions?.length}
				<PlayChoices
					versions={t.versions}
					bind:version
					bind:audio
					bind:subtitle
				/>
			{/if}
		</div>
	</header>

	{#if t.season &&
		episodes &&
		(episodes.length > 1 || (data.seasons?.length ?? 0) > 1)}
		<Rail
			title={picked?.title ?? t.season.title}
			cards={episodes}
			shape="still"
			current={t.id}
			caption={(i: number) =>
				episodeLabel(
					picked?.number ?? t.season_number,
					episodes[i].episode_number,
					episodes[i].episode_end,
				)}
		>
			{#snippet heading()}
				{#if data.seasons && data.seasons.length > 1}
					<Choice
						options={data.seasons.map((s) => ({ value: s.id, label: s.title }))}
						value={picked?.id ?? t.season?.id}
						onchange={pick}
						class="h-8 w-auto gap-2 rounded-full border-none bg-white/6 pr-2 pl-3 font-semibold hover:bg-white/12"
					/>
				{:else}
					{t.season?.title}
				{/if}
			{/snippet}
		</Rail>
	{/if}

	{#if data.members}
		<section aria-labelledby="members">
			<h2 id="members" class="heading mb-3">
				{count(data.members.length, "title")}
			</h2>
			<CardGrid cards={data.members} />
		</section>
	{/if}

	{#if credits.length}
		<Rail title="Cast & crew" items={credits} href="/titles/{t.id}/cast">
			{#snippet card(
				credit: (typeof credits)[number],
			)}
				<PersonCard
					id={credit.person_id}
					name={credit.name}
					photo={credit.photo}
					blurhashes={credit.blurhashes}
					caption={credit.said}
				/>
			{/snippet}
		</Rail>
	{/if}

	{#if extras.items.length}
		<Rail
			title="Extras"
			shape="still"
			items={extras.items}
			href="/titles/{extras.of}/extras"
		>
			{#snippet card(
				item: Extra,
				sizes: string,
			)}
				<ExtraCard {item} {sizes} />
			{/snippet}
		</Rail>
	{/if}

	{#if collections.length}
		<Rail
			title="Collections"
			cards={collections}
			href="/titles/{about.id}/collections"
		/>
	{/if}

	{#await data.similar then similar}
		{#if similar?.length}
			<Rail
				title="More like this"
				cards={similar}
				href="/titles/{about.id}/similar"
			/>
		{/if}
	{/await}

	{#if t.studios?.length ||
		directors.length ||
		writers.length ||
		t.original_title ||
		links.length}
		<section aria-labelledby="details">
			<h2 id="details" class="heading mb-3">Details</h2>
			<dl class="grid max-w-3xl grid-cols-[auto_1fr] gap-x-6 gap-y-2 text-sm">
				{#if t.original_title && t.original_title !== t.title}
					<dt class="text-ink-3">Original title</dt>
					<dd class="text-ink">{t.original_title}</dd>
				{/if}
				{#each [
					["Directed by", directors],
					["Written by", writers],
				] as const as [name, people] (name)}
					{#if people.length}
						<dt class="text-ink-3">{name}</dt>
						<dd class="text-ink">
							{#each people as person, i (person.person_id)}
								{i ? ", " : ""}<a
									href="/people/{person.person_id}"
									class="hover:underline"
									>{person.name}</a
								>
							{/each}
						</dd>
					{/if}
				{/each}
				{#if t.studios?.length}
					<dt class="text-ink-3">
						{t.kind === "show" ? "Networks" : "Studios"}
					</dt>
					<dd class="text-ink">{t.studios.join(", ")}</dd>
				{/if}
				{#if links.length}
					<dt class="text-ink-3">Elsewhere</dt>
					<dd class="flex flex-wrap gap-x-3">
						{#each links as link (link.name)}
							<a
								href={link.href}
								target="_blank"
								rel="noopener noreferrer"
								class="text-ink inline-flex items-center gap-1 hover:underline"
							>
								{link.name}
								<ExternalLinkIcon class="size-3" aria-hidden="true" />
								<span class="sr-only">(opens in a new tab)</span>
							</a>
						{/each}
					</dd>
				{/if}
			</dl>
		</section>
	{/if}
</article>

{#if t.versions?.length}
	<MediaInfo bind:open={mediaInfo} title={t.title} versions={t.versions} />
{/if}
{#if playable && chosen}
	<DownloadDialog
		bind:open={download}
		id={t.id}
		title={t.title}
		version={chosen}
	/>
{/if}
