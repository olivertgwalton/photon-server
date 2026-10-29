<script lang="ts" module>
import { client } from "#lib/api/client.js";

type Rating = components["schemas"]["RatingRef"];
type Lent = { genres: string[]; ratings: Rating[] };

// What a show lends its episodes, asked for once a show however often its
// episodes are rested on.
const shows = new Map<string, Promise<Lent>>();
function lentBy(id: string): Promise<Lent> {
	let lent = shows.get(id);
	if (!lent) {
		lent = client()
			.GET("/api/v1/titles/{id}", { params: { path: { id } } })
			.then(({ data }) => ({
				genres: data?.genres ?? [],
				ratings: data?.ratings ?? [],
			}));
		shows.set(id, lent);
	}
	return lent;
}
</script>

<script lang="ts">
import InfoIcon from "@lucide/svelte/icons/info";
import PlayIcon from "@lucide/svelte/icons/play";
import type { components } from "#lib/api/schema.js";
import { blurStyle, isDark, tintColour } from "#lib/blurhash.js";
import Artwork from "#lib/components/Artwork.svelte";
import Prose from "#lib/components/Prose.svelte";
import RatingScore from "#lib/components/RatingScore.svelte";
import { Badge } from "#lib/components/ui/badge/index.js";
import { Button } from "#lib/components/ui/button/index.js";
import {
	episodeLabel,
	playHref,
	resumeLabel,
	runtime,
	titleWithShow,
} from "#lib/format.js";

// One title of Home's showcase, as the apps' column draws it over its
// backdrop: the lettering, what the title is (an episode by its place, then
// its name), two lines of its write-up on a plate, how long it runs or what
// is left of it with its certificate and scores, then Play and More Info.
let { card }: { card: components["schemas"]["Card"] } = $props();

const playable = $derived(card.kind === "movie" || card.kind === "episode");
// Where the reader is part way through, as the apps judge it: a few seconds
// in is a false start, and the last minute is the credits.
const position = $derived.by(() => {
	const at = card.state?.position_ms ?? 0;
	const length = card.duration_ms ?? 0;
	return at > falseStartMs && at < length - creditsMs ? at : 0;
});
// What is left where the reader is part way through, else how long it runs.
const time = $derived(
	card.duration_ms &&
		(position
			? `${runtime(card.duration_ms - position)} left`
			: runtime(card.duration_ms)),
);
const place = $derived(
	card.kind === "episode"
		? episodeLabel(card.season_number, card.episode_number, card.episode_end)
		: undefined,
);
// An episode short of genres wears its show's, and every site that did not
// score it lends its show's score.
let lent = $state<Lent>();
$effect(() => {
	const show = card.kind === "episode" ? card.show?.id : undefined;
	lent = undefined;
	if (!show) return;
	let current = true;
	lentBy(show).then(
		(l) => {
			if (current) lent = l;
		},
		() => {},
	);
	return () => {
		current = false;
	};
});
const genres = $derived(
	card.genres?.length ? card.genres : (lent?.genres ?? []),
);
const ratings = $derived([
	...(card.ratings ?? []),
	...(lent?.ratings ?? []).filter(
		(r) => !card.ratings?.some((o) => o.site === r.site),
	),
]);
// When it came out: an episode by the day it aired, anything else its year.
const released = $derived(
	place && card.release_date
		? new Date(card.release_date).toLocaleDateString(undefined, {
				day: "numeric",
				month: "short",
				year: "numeric",
				timeZone: "UTC",
			})
		: card.year,
);
const source = $derived(
	[released, ...genres.slice(0, 3)].filter(Boolean).join(" · "),
);
// An episode's lettering is its show's, and names it by its show.
const named = $derived(card.show?.title ?? card.title);

const backdrop = $derived(card.backdrop);
const accent = $derived(
	(backdrop && tintColour(card.blurhashes?.[backdrop])) ?? "transparent",
);
const logo = $derived(card.logo);
const darkLogo = $derived(logo ? isDark(card.blurhashes?.[logo]) : false);

const falseStartMs = 30_000;
const creditsMs = 60_000;

// Beside Play, a pane of glass over the backdrop, as on a title's page.
const glass =
	"rounded-full border-white/10 bg-white/8 backdrop-blur-xl hover:bg-white/14";
</script>

<div
	class="relative grid min-h-[max(30rem,62svh)] content-end"
	style:--accent={accent}
>
	{#if backdrop}
		<div
			class="absolute inset-0 overflow-hidden"
			style={blurStyle(card.blurhashes?.[backdrop])}
		>
			{#key backdrop}
				<Artwork
					id={backdrop}
					shape="backdrop"
					loading="eager"
					sizes="100vw"
					class="size-full object-cover object-top transition-opacity duration-700 data-loading:opacity-0"
				/>
			{/key}
			<div
				class="from-ground via-(--accent)/35 absolute inset-0 bg-linear-to-t to-transparent"
			></div>
			<div
				class="from-ground/90 absolute inset-0 bg-linear-to-r via-transparent to-transparent max-sm:hidden"
			></div>
		</div>
	{/if}
	<!-- Another title fades its writing in where the last stood. -->
	{#key card.id}
		<div
			class="relative grid max-w-180 justify-items-center gap-4 px-3 pt-24 pb-6 text-center motion-safe:animate-in motion-safe:fade-in motion-safe:duration-500 sm:justify-items-start sm:px-6 sm:text-left"
		>
			<h2>
				{#if logo}
					<Artwork
						id={logo}
						shape="still"
						loading="eager"
						sizes="26rem"
						alt={named}
						class="h-20 w-[min(17.5rem,80vw)] object-contain transition-opacity duration-500 data-loading:opacity-0 sm:h-28 sm:w-104 sm:object-left {darkLogo
							? "lettering-glow"
							: ""}"
					/>
				{:else}
					<span class="title block text-3xl md:text-5xl">{named}</span>
				{/if}
			</h2>
			{#if place}
				<div class="grid gap-1">
					<p class="text-ink-2 text-sm">
						{[place, source].filter(Boolean).join(" · ")}
					</p>
					<p class="text-ink font-heading text-lg">{card.title}</p>
				</div>
			{:else if source}
				<p class="text-ink-2 text-sm">{source}</p>
			{/if}
			{#if card.overview}
				<Prose
					text={card.overview}
					lines={2}
					title={titleWithShow(card)}
					class="text-ink-2 rounded-xl bg-white/8 px-4 py-3 text-left backdrop-blur-xl"
				/>
			{/if}
			{#if time || card.certificate || ratings.length}
				<ul
					class="text-ink-2 flex flex-wrap items-center justify-center gap-2 text-sm sm:justify-start"
					aria-label="Facts"
				>
					{#if time}
						<li class={[position && "text-ink"]}>{time}</li>
					{/if}
					{#if card.certificate}
						<li><Badge variant="outline">{card.certificate}</Badge></li>
					{/if}
					{#each ratings as rating (rating.site)}
						<li
							class="flex rounded-full bg-white/8 px-3 py-1.25 backdrop-blur-xl"
						>
							<RatingScore {rating} />
						</li>
					{/each}
				</ul>
			{/if}
			<div class="flex flex-wrap justify-center gap-2 pt-1">
				{#if playable}
					<Button href={playHref(card.id)} size="lg" class="rounded-full px-5">
						<PlayIcon class="fill-current" />
						{position ? resumeLabel(position) : "Play"}
						<span class="sr-only">{titleWithShow(card)}</span>
					</Button>
				{/if}
				<Button
					href="/titles/{card.id}"
					variant="ghost"
					size="lg"
					class={glass}
				>
					<InfoIcon />More Info
					<span class="sr-only">about {titleWithShow(card)}</span>
				</Button>
			</div>
		</div>
	{/key}
</div>
