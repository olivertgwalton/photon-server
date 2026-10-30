<script lang="ts">
import type { components } from "#lib/api/schema.d.ts";
import anidb from "#lib/assets/ratings/anidb.png";
import anilist from "#lib/assets/ratings/anilist.svg";
import imdb from "#lib/assets/ratings/imdb.svg";
import popcornSpilled from "#lib/assets/ratings/popcorn-spilled.svg";
import popcornUpright from "#lib/assets/ratings/popcorn-upright.svg";
import tmdb from "#lib/assets/ratings/tmdb.svg";
import tomatometerFresh from "#lib/assets/ratings/tomatometer-fresh.svg";
import tomatometerRotten from "#lib/assets/ratings/tomatometer-rotten.svg";
import { type RatingMark, ratingMark, score } from "#lib/format.ts";
import { vocabulary } from "#lib/vocabulary.ts";

let { rating }: { rating: components["schemas"]["RatingRef"] } = $props();
const words = vocabulary();

const marks: Record<RatingMark, string> = {
	imdb,
	tmdb,
	"tomatometer-fresh": tomatometerFresh,
	"tomatometer-rotten": tomatometerRotten,
	"popcorn-upright": popcornUpright,
	"popcorn-spilled": popcornSpilled,
	anilist,
	anidb,
};

const mark = $derived(ratingMark(rating.site, rating.score));
// The wordmarks sit at the numbers' height; the tomato, popcorn and AniList's
// and AniDB's icons take the line's.
const wordmark = $derived(mark === "imdb" || mark === "tmdb");
</script>

<span
	class="inline-flex items-center gap-1.5"
	title={rating.votes ? `${rating.votes.toLocaleString()} votes` : undefined}
>
	<img
		src={marks[mark]}
		alt={words.rating_sites[rating.site]}
		class={wordmark ? "h-3.5 w-auto" : "h-4 w-auto"}
	>
	<span class="text-ink font-semibold">{score(rating.site, rating.score)}</span>
</span>
