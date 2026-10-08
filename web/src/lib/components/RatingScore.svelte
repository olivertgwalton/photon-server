<script lang="ts">
import imdb from "#lib/assets/ratings/imdb.svg";
import popcornSpilled from "#lib/assets/ratings/popcorn-spilled.svg";
import popcornUpright from "#lib/assets/ratings/popcorn-upright.svg";
import tmdb from "#lib/assets/ratings/tmdb.svg";
import tomatometerFresh from "#lib/assets/ratings/tomatometer-fresh.svg";
import tomatometerRotten from "#lib/assets/ratings/tomatometer-rotten.svg";
import type { components } from "#lib/api/schema.js";
import {
	type RatingMark,
	ratingMark,
	ratingSites,
	score,
} from "#lib/format.js";

let { rating }: { rating: components["schemas"]["RatingRef"] } = $props();

const marks: Record<RatingMark, string> = {
	imdb,
	tmdb,
	"tomatometer-fresh": tomatometerFresh,
	"tomatometer-rotten": tomatometerRotten,
	"popcorn-upright": popcornUpright,
	"popcorn-spilled": popcornSpilled,
};

const mark = $derived(ratingMark(rating.site, rating.score));
// The wordmarks sit at the numbers' height; the tomato and popcorn take the line's.
const wordmark = $derived(mark === "imdb" || mark === "tmdb");
</script>

<span
	class="inline-flex items-center gap-1.5"
	title={rating.votes ? `${rating.votes.toLocaleString()} votes` : undefined}
>
	<img
		src={marks[mark]}
		alt={ratingSites[rating.site]}
		class={wordmark ? "h-3.5 w-auto" : "h-4 w-auto"}
	>
	<span class="text-ink font-semibold">{score(rating.site, rating.score)}</span>
</span>
