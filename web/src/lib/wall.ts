import type { components, paths } from "./api/schema.js";

type Schemas = components["schemas"];

// What a wall is asked for, as the address carries it, so a narrowed wall can
// be linked to and comes back as it was left.
export type WallQuery = NonNullable<
	paths["/api/v1/libraries/{id}/titles"]["get"]["parameters"]["query"]
>;

// The sorts a wall offers, in the order its menu lists them.
export const sorts: Schemas["WallSort"][] = [
	"title",
	"added",
	"released",
	"rating",
	"runtime",
	"played",
];
const sites: Schemas["RatingSite"][] = [
	"imdb",
	"tmdb",
	"rotten_tomatoes",
	"rotten_tomatoes_audience",
];
const lists = [
	"mark",
	"genre",
	"year",
	"certificate",
	"studio",
	"resolution",
	"range",
	"person",
] as const;

export type ListFilter = (typeof lists)[number];

// The lists whose values are the server's own words.
const known: Partial<Record<ListFilter, string[]>> = {
	mark: [
		"watched",
		"unwatched",
		"in_progress",
		"favourite",
		"watchlist",
	] satisfies Schemas["Mark"][],
	resolution: ["sd", "720p", "1080p", "4k"] satisfies Schemas["Resolution"][],
	range: [
		"sdr",
		"hlg",
		"hdr10",
		"hdr10plus",
		"dv",
	] satisfies Schemas["Range"][],
};

const kinds: Schemas["WallKind"][] = ["titles", "films", "shows", "episodes"];

const uuid = /^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$/i;

// The wall's query from an address. Anything the server would refuse is left
// out rather than sent, so a mistyped link opens the plain wall.
export function wallQuery(search: URLSearchParams): WallQuery {
	const query: WallQuery = {};
	const kind = search.get("kind") as Schemas["WallKind"];
	if (kinds.includes(kind) && kind !== "titles") query.kind = kind;
	const sort = search.get("sort") as Schemas["WallSort"];
	if (sorts.includes(sort)) query.sort = sort;
	const order = search.get("order");
	if (order === "asc" || order === "desc") query.order = order;
	const site = search.get("rating_site") as Schemas["RatingSite"];
	if (sites.includes(site)) query.rating_site = site;
	const min = Number(search.get("min_rating"));
	if (min > 0 && min <= 100) query.min_rating = min;
	for (const name of lists) {
		const values = search
			.getAll(name)
			.flatMap((v) => v.split(","))
			.filter((v) =>
				name === "person"
					? uuid.test(v)
					: v && (known[name]?.includes(v) ?? true),
			);
		if (!values.length) continue;
		if (name === "year") {
			const years = values.map(Number).filter(Number.isInteger);
			if (years.length) query.year = years;
		} else {
			(query as Record<string, string[]>)[name] = values;
		}
	}
	return query;
}

// The address for a query, its lists comma-joined as the server reads them.
export function wallSearch(query: WallQuery): string {
	const out = new URLSearchParams();
	for (const [name, value] of Object.entries(query)) {
		if (value == null || name === "offset" || name === "limit") continue;
		if (Array.isArray(value)) {
			if (value.length) out.set(name, value.join(","));
		} else out.set(name, String(value));
	}
	const s = out.toString();
	return s ? `?${s}` : "";
}

// Turns one value of a list filter on or off.
export function toggled(
	query: WallQuery,
	name: ListFilter,
	value: string | number,
): WallQuery {
	const now = (query[name] ?? []) as (string | number)[];
	const next = now.includes(value)
		? now.filter((v) => v !== value)
		: [...now, value];
	return { ...query, [name]: next.length ? next : undefined };
}

export function filterCount(query: WallQuery): number {
	return (
		lists.reduce((n, name) => n + (query[name]?.length ?? 0), 0) +
		(query.min_rating ? 1 : 0)
	);
}

// Only what narrows: what the wall holds and its order are the reader's to keep.
export function cleared(query: WallQuery): WallQuery {
	return {
		kind: query.kind,
		sort: query.sort,
		order: query.order,
		rating_site: query.sort === "rating" ? query.rating_site : undefined,
	};
}

// Where the wall stands at a letter: the sum of every letter before it, in the
// order the wall is drawn. `#` (anything before A) is first going up.
export function letterOffset(
	letters: Schemas["Letter"][],
	letter: string,
	order: Schemas["Order"],
): number {
	const ranked = order === "desc" ? [...letters].reverse() : letters;
	let offset = 0;
	for (const l of ranked) {
		if (l.letter === letter) return offset;
		offset += l.count;
	}
	return offset;
}

// What a wall is narrowed and ordered by, as a rule holds it. A letter is
// where the wall was scrolled to rather than part of what it holds, so a rule
// never carries one.
function ruleOf(query: WallQuery): Schemas["SmartRule"] {
	return {
		filter: {
			marks: query.mark,
			genres: query.genre,
			years: query.year,
			certificates: query.certificate,
			studios: query.studio,
			resolutions: query.resolution,
			ranges: query.range,
			people: query.person,
			rating_site: query.rating_site,
			min_rating: query.min_rating,
		},
		sort: query.sort,
		order: query.order,
	};
}

// A wall's query as a smart collection's rule, as Plex saves a filtered
// library as a smart collection; none where it reads a profile's own marks or
// plays, which a collection everyone shares cannot, or pages one kind of a
// library's items, which a rule over all its titles cannot keep to.
export function smartRule(query: WallQuery): Schemas["SmartRule"] | undefined {
	if (query.mark?.length || query.sort === "played" || query.kind)
		return undefined;
	return ruleOf(query);
}

// A wall's query as a smart playlist's rule, as Plex saves a filtered library
// as a smart playlist. A playlist plays its entries one after another, so only
// a wall of films or of episodes is one to keep, a films library's whole wall
// being its films. The rule is one profile's own, so unlike a collection's it
// may read that profile's marks and plays.
export function playlistRule(
	query: WallQuery,
	library: Schemas["LibraryKind"],
): Schemas["SmartRule"] | undefined {
	const kind = query.kind ?? (library === "movies" ? "films" : undefined);
	if (kind !== "films" && kind !== "episodes") return undefined;
	return { ...ruleOf(query), kind };
}

// The wall a smart collection's or a smart playlist's rule reads, to open and
// change it there.
export function ruleQuery(rule: Schemas["SmartRule"]): WallQuery {
	const f = rule.filter;
	return Object.fromEntries(
		Object.entries({
			kind: rule.kind,
			sort: rule.sort,
			order: rule.order,
			mark: f.marks,
			genre: f.genres,
			year: f.years,
			certificate: f.certificates,
			studio: f.studios,
			resolution: f.resolutions,
			range: f.ranges,
			person: f.people,
			rating_site: f.rating_site,
			min_rating: f.min_rating,
		}).filter(([, v]) => v != null && !(Array.isArray(v) && !v.length)),
	);
}

export const viewStyles = ["poster", "still", "list"] as const;
export type ViewStyle = (typeof viewStyles)[number];

// How many cards a wall asks for at a time: a few screens of posters.
export const wallPageSize = 100;

// Where this browser keeps how a library's wall is drawn.
export function viewKey(library: string): string {
	return `photon:view:${library}`;
}
