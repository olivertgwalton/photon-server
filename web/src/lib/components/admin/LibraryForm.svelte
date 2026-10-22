<script lang="ts">
import { offeredSources, serverLocale } from "#lib/admin/library.js";
import type { components } from "#lib/api/schema.js";
import Choice from "#lib/components/Choice.svelte";
import { Checkbox } from "#lib/components/ui/checkbox/index.js";
import * as Field from "#lib/components/ui/field/index.js";
import { Input } from "#lib/components/ui/input/index.js";
import { Label } from "#lib/components/ui/label/index.js";
import { country, language, localeOptions } from "#lib/format.js";
import { subtitleLanguages } from "#lib/player/words.js";
import { vocabulary } from "#lib/vocabulary.js";
import FolderPicker from "./FolderPicker.svelte";
import SourceRanker from "./SourceRanker.svelte";

type Schemas = components["schemas"];

// What a library is and how it is kept: the fields of a new library and of
// one being changed. A library's kind and folder are fixed once it is made.
let {
	library,
	kinds,
	providers,
	locales,
	serverLanguage,
	serverCountry,
}: {
	library?: Schemas["AdminLibrary"];
	kinds: Schemas["LibraryKindOptions"][];
	providers: Schemas["MetadataProvider"][];
	locales: Schemas["Locales"];
	// The server's own metadata language and certification country, which a
	// library asks in by default.
	serverLanguage: string;
	serverCountry?: string;
} = $props();

const id = $props.id();
const words = vocabulary();

const languages = $derived([
	{
		value: serverLocale,
		label: `Server default (${language(serverLanguage)})`,
	},
	...localeOptions(locales.languages),
]);
const countries = $derived([
	{
		value: serverLocale,
		label: `Automatic${serverCountry ? ` (${country(serverCountry)})` : ""}`,
	},
	...localeOptions(locales.countries, country),
]);

const defaults = {
	remote_extras: ["trailer", "featurette", "behind_the_scenes"],
	monitor: "realtime",
	refresh_days: 30,
	previews: "all",
	markers: "all",
	keyframes: "index",
	themes: "local",
	deletion: "off",
	missing_versions: "keep",
	artwork_language: "localized",
	title_language: "localized",
	box_sets: "expanded",
	subtitle_match: "release",
} as const;

// A new library's kind changes the sources offered; a library's own is fixed.
let kind = $derived<Schemas["LibraryKind"]>(library?.kind ?? "movies");

// Where its media is: files under a folder, or what a provider streams of the
// titles of a list, as a remote library plays; fixed once it is made.
let media = $derived<Schemas["LibraryMedia"]>(library?.media ?? "folder");
const remote = $derived(media === "remote");
const able = (
	c: Schemas["Capability"],
	also: (p: Schemas["MetadataProvider"]) => boolean = () => true,
) =>
	providers
		.filter((p) => p.capabilities.includes(c) && also(p))
		.map((p) => ({ value: p.id, label: p.name }));
// A remote library holds a list's titles, what a search finds, or both: those
// a provider files under ids of its own, which a title found is kept by.
const listers = $derived([{ value: "", label: "No list" }, ...able("list")]);
const streamers = $derived(able("stream"));
const searchers = $derived([
	{ value: "", label: "Nothing" },
	...able("search").filter(
		(p) => p.value.startsWith("plugin:") || ["tmdb", "tvdb"].includes(p.value),
	),
]);
// A library of files' requests go to a provider that fetches every kind of
// title it holds, or to an admin, by hand.
const holds = $derived(
	({ movies: ["movie"], shows: ["show"], mixed: ["movie", "show"] } as const)[
		kind
	],
);
const requesters = $derived([
	{ value: "", label: "Nobody: you add them yourself" },
	...able("request", (p) => holds.every((k) => p.kinds.includes(k))),
]);
let listSource = $state("");
const nameOf = (id?: Schemas["FieldSource"]) =>
	providers.find((p) => p.id === id)?.name ?? id;

const fetchers = [
	{
		f: "metadata",
		title: "Metadata downloaders",
		said: "Enable and rank your preferred metadata downloaders in order of priority. Lower priority downloaders will only be used to fill in missing information.",
	},
	{
		f: "images",
		title: "Image fetchers",
		said: "Enable and rank your preferred image fetchers in order of priority.",
	},
] as const;

// What a library of its kind ranks sources for, and what a new one ranks.
const options = $derived(kinds.find((k) => k.kind === kind));
const chosen = (made: Schemas["KindSources"]) =>
	library?.sources.find((s) => s.kind === made.kind) ?? made;

const extras = $derived<readonly string[]>(
	library?.remote_extras ?? defaults.remote_extras,
);

// The languages offered: the common ones, and any the library already has.
const fetching = $derived(library?.subtitle_languages ?? []);
const offeredLanguages = $derived(
	localeOptions(new Set([...subtitleLanguages, ...fetching])),
);

const refreshing = [
	{ value: "0", label: "Never" },
	{ value: "7", label: "Every week" },
	{ value: "30", label: "Every 30 days" },
	{ value: "60", label: "Every 60 days" },
	{ value: "90", label: "Every 90 days" },
];
const refresh = $derived(
	String(library?.refresh_days ?? defaults.refresh_days),
);
const refreshOptions = $derived(
	refreshing.some((r) => r.value === refresh)
		? refreshing
		: [...refreshing, { value: refresh, label: `Every ${refresh} days` }],
);
</script>

<Field.Group>
	<Field.Field>
		<Field.Label for="{id}-library-name">Name</Field.Label>
		<Input
			id="{id}-library-name"
			name="name"
			required
			value={library?.name ?? ""}
		/>
	</Field.Field>

	{#if !library}
		<Field.Field>
			<Field.Label for="{id}-library-kind">Holds</Field.Label>
			<Choice
				id="{id}-library-kind"
				name="kind"
				bind:value={kind}
				options={Object.entries(words.library_kinds).map(([value, label]) => ({
					value,
					label,
				}))}
			/>
		</Field.Field>
		<Field.Field>
			<Field.Label for="{id}-library-media">Its media</Field.Label>
			<Choice
				id="{id}-library-media"
				name="media"
				bind:value={media}
				options={[
					{ value: "folder", label: "Files in a folder" },
					{
						value: "remote",
						label: "Streamed by a provider",
						disabled: !streamers.length,
					},
				]}
			/>
			{#if !streamers.length}
				<Field.Description>
					Streaming needs a provider that streams, such as a Stremio addon
					registered under Providers.
				</Field.Description>
			{/if}
		</Field.Field>
		{#if remote}
			<div class="grid gap-4 sm:grid-cols-[auto_1fr]">
				<Field.Field>
					<Field.Label for="{id}-library-list-source">Titles from</Field.Label>
					<Choice
						id="{id}-library-list-source"
						name="list_source"
						bind:value={listSource}
						options={listers}
						class="w-48"
					/>
				</Field.Field>
				{#if listSource}
					<Field.Field>
						<Field.Label for="{id}-library-list">List</Field.Label>
						<Input
							id="{id}-library-list"
							name="list_id"
							required
							placeholder="movie/top"
							class="font-mono"
						/>
					</Field.Field>
				{/if}
			</div>
			<Field.Field>
				<Field.Label for="{id}-library-discover"
					>Search finds titles from</Field.Label
				>
				<Choice
					id="{id}-library-discover"
					name="discover"
					value={searchers[1]?.value ?? ""}
					options={searchers}
				/>
				<Field.Description>
					A search of the server shows what this finds that the library does not
					hold yet; opened, a title becomes the library's.
				</Field.Description>
			</Field.Field>
			<Field.Field>
				<Field.Label for="{id}-library-streams">Streamed by</Field.Label>
				<Choice
					id="{id}-library-streams"
					name="streams"
					value={streamers[0]?.value}
					options={streamers}
				/>
				<Field.Description>
					A title's streams are asked for as it is opened, and the best one that
					plays is read and kept as its copy.
				</Field.Description>
			</Field.Field>
		{:else}
			<Field.Field>
				<Field.Label for="{id}-library-root">Folder</Field.Label>
				<FolderPicker id="{id}-library-root" name="root" />
				<Field.Description>
					A folder on the server. Everything under it is scanned.
				</Field.Description>
			</Field.Field>
		{/if}
	{:else if library.media === "remote"}
		{#if library.list}
			<Field.Field>
				<Field.Label for="{id}-library-list">Titles from</Field.Label>
				<Input
					id="{id}-library-list"
					value="{nameOf(library.list.source)}: {library.list.id}"
					readonly
					class="font-mono"
				/>
			</Field.Field>
		{/if}
		{#if library.discover}
			<Field.Field>
				<Field.Label for="{id}-library-discover"
					>Search finds titles from</Field.Label
				>
				<Input
					id="{id}-library-discover"
					value={nameOf(library.discover)}
					readonly
				/>
			</Field.Field>
		{/if}
		<Field.Field>
			<Field.Label for="{id}-library-streams">Streamed by</Field.Label>
			<Input
				id="{id}-library-streams"
				value={nameOf(library.streams)}
				readonly
			/>
		</Field.Field>
	{:else}
		<Field.Field>
			<Field.Label for="{id}-library-root">Folder</Field.Label>
			<Input
				id="{id}-library-root"
				value={library.root}
				readonly
				class="font-mono"
			/>
		</Field.Field>
		<Field.Field>
			<Field.Label for="{id}-library-discover"
				>People can ask for titles from</Field.Label
			>
			<Choice
				id="{id}-library-discover"
				name="discover"
				value={library.discover ?? ""}
				options={searchers}
			/>
			<Field.Description>
				A search of the server shows what this finds that no library holds, for
				people to ask for. Nothing turns asking off.
			</Field.Description>
		</Field.Field>
		<Field.Field>
			<Field.Label for="{id}-library-requests">Requests go to</Field.Label>
			<Choice
				id="{id}-library-requests"
				name="requests"
				value={library.requests ?? ""}
				options={requesters}
			/>
			<Field.Description>
				Once you, or the manager who keeps the profile that asked, approve a
				request, it is sent here to fetch. Whoever asked is told once a scan
				adds the title.
			</Field.Description>
		</Field.Field>
	{/if}

	<!-- A new library needs only its folder; the rest has defaults that suit most,
	     each changed later in its settings. -->
	<details open={!!library}>
		<summary class="text-ink-2 cursor-pointer text-sm font-medium">
			More settings
		</summary>
		<Field.Group class="mt-7">
			{#key kind}
				{#each options?.sources ?? [] as made (made.kind)}
					{@const item = made.kind}
					{@const fetchable = options?.fetchable.find((x) => x.kind === item)}
					{#each fetchers as { f, title, said } (f)}
						{@const id = `sources-${item}-${f}`}
						<Field.Set>
							<Field.Legend id="{id}-legend"
								>{title}
								({words.kinds[item]})</Field.Legend
							>
							<Field.Description>{said}</Field.Description>
							<SourceRanker
								name={id}
								labelledby="{id}-legend"
								offered={offeredSources(
									providers,
									fetchable?.[f] ?? [],
									f,
									item,
								)}
								chosen={chosen(made)[f]}
							/>
						</Field.Set>
					{/each}
				{/each}
				<Field.Description>
					Changing these identifies every title in the library again.
				</Field.Description>
			{/key}

			<Field.Set>
				<Field.Legend>Videos to link from the providers</Field.Legend>
				<div class="grid gap-2 sm:grid-cols-3">
					{#each Object.entries(words.extras) as [value, label] (value)}
						<div class="flex items-center gap-2">
							<Checkbox
								id="{id}-extra-{value}"
								name="remote_extras"
								{value}
								checked={extras.includes(value)}
							/>
							<Label for="{id}-extra-{value}">{label}</Label>
						</div>
					{/each}
				</div>
			</Field.Set>

			<Field.Set>
				<Field.Legend>Subtitles to download</Field.Legend>
				<Field.Description>
					Copies with no subtitle in a language ticked have one fetched from the
					subtitle providers when they are added, and daily after.
				</Field.Description>
				<div class="grid grid-cols-2 gap-2 sm:grid-cols-4">
					{#each offeredLanguages as { value: tag, label } (tag)}
						<div class="flex items-center gap-2">
							<Checkbox
								id="{id}-subtitle-{tag}"
								name="subtitle_languages"
								value={tag}
								checked={fetching.includes(tag)}
							/>
							<Label for="{id}-subtitle-{tag}">{label}</Label>
						</div>
					{/each}
				</div>
				<Field.Field>
					<Field.Label for="{id}-library-subtitle-match">Download</Field.Label>
					<Choice
						id="{id}-library-subtitle-match"
						name="subtitle_match"
						value={library?.subtitle_match ?? defaults.subtitle_match}
						options={[
							{ value: "release", label: "Only those made for the file" },
							{ value: "any", label: "The best found" },
						]}
					/>
					<Field.Description>
						One made for another release of the title may be out of time with
						this one.
					</Field.Description>
				</Field.Field>
			</Field.Set>

			<div class="grid gap-4 sm:grid-cols-2">
				{#if !remote}
					<Field.Field>
						<Field.Label for="{id}-library-monitor"
							>Watch for changes</Field.Label
						>
						<Choice
							id="{id}-library-monitor"
							name="monitor"
							value={library?.monitor ?? defaults.monitor}
							options={[
								{ value: "realtime", label: "As they happen" },
								{ value: "off", label: "Only when scanned" },
							]}
						/>
					</Field.Field>
				{/if}
				<Field.Field>
					<Field.Label for="{id}-library-refresh">Refresh metadata</Field.Label>
					<Choice
						id="{id}-library-refresh"
						name="refresh_days"
						value={refresh}
						options={refreshOptions}
					/>
				</Field.Field>
				<Field.Field>
					<Field.Label for="{id}-library-previews">Previews</Field.Label>
					<Choice
						id="{id}-library-previews"
						name="previews"
						value={library?.previews ?? (remote ? "off" : defaults.previews)}
						options={[
							{ value: "all", label: "Chapters and seeking" },
							{ value: "chapters", label: "Chapter images only" },
							{ value: "off", label: "None" },
						]}
					/>
				</Field.Field>
				<Field.Field>
					<Field.Label for="{id}-library-markers"
						>Intros and credits</Field.Label
					>
					<Choice
						id="{id}-library-markers"
						name="markers"
						value={library?.markers ?? (remote ? "off" : defaults.markers)}
						options={[
							{ value: "all", label: "From chapters, sound and picture" },
							{ value: "chapters", label: "From chapters only" },
							{ value: "off", label: "Not looked for" },
						]}
					/>
					<Field.Description>
						Comparing sound reads the start and end of every episode; a film's
						credits are found where its picture goes dark near its end.
					</Field.Description>
				</Field.Field>
				<Field.Field>
					<Field.Label for="{id}-library-keyframes">Keyframes</Field.Label>
					<Choice
						id="{id}-library-keyframes"
						name="keyframes"
						value={library?.keyframes ?? defaults.keyframes}
						options={[
							{ value: "index", label: "From the file's index" },
							{ value: "full", label: "Read every file through" },
							{ value: "off", label: "Not looked for" },
						]}
					/>
					<Field.Description>
						The index is a few small reads, right for a network share; reading
						through, in the maintenance window, gives exact segments from a
						local disk.
					</Field.Description>
				</Field.Field>
				<Field.Field>
					<Field.Label for="{id}-library-themes">Theme music</Field.Label>
					<Choice
						id="{id}-library-themes"
						name="themes"
						value={library?.themes ?? defaults.themes}
						options={[
							{ value: "local", label: "Local files only" },
							{ value: "themerr", label: "Local files and ThemerrDB's" },
							{ value: "off", label: "None" },
						]}
					/>
					<Field.Description>
						A theme.mp3 or a theme-music folder beside a title. With
						ThemerrDB's, a film or show with neither downloads the theme audio
						from the YouTube link ThemerrDB lists for it, which needs yt-dlp on
						the server.
					</Field.Description>
				</Field.Field>
				<Field.Field>
					<Field.Label for="{id}-library-language"
						>Metadata language</Field.Label
					>
					<Choice
						id="{id}-library-language"
						name="metadata_language"
						value={library?.metadata_language ?? serverLocale}
						options={languages}
					/>
					<Field.Description>
						What its titles' names, write-ups and pictures are asked for in.
						Changing it describes them all again.
					</Field.Description>
				</Field.Field>
				<Field.Field>
					<Field.Label for="{id}-library-titles">Titles</Field.Label>
					<Choice
						id="{id}-library-titles"
						name="title_language"
						value={library?.title_language ?? defaults.title_language}
						options={[
							{ value: "localized", label: "In its language" },
							{ value: "original", label: "As first named" },
						]}
					/>
					<Field.Description>
						As first named keeps a film's or show's original title, its write-up
						still in its language.
					</Field.Description>
				</Field.Field>
				<Field.Field>
					<Field.Label for="{id}-library-box-sets">Box sets</Field.Label>
					<Choice
						id="{id}-library-box-sets"
						name="box_sets"
						value={library?.box_sets ?? defaults.box_sets}
						options={[
							{ value: "expanded", label: "Each title on its own" },
							{ value: "collapsed", label: "One tile for the set" },
							{ value: "beside", label: "The set and each title" },
						]}
					/>
					<Field.Description>
						One tile for the set shows two or more titles of a collection, such
						as a film series, as the collection in their place. The set and each
						title adds the collection beside them. A filtered library shows each
						title.
					</Field.Description>
				</Field.Field>
				<Field.Field>
					<Field.Label for="{id}-library-artwork">Pictures</Field.Label>
					<Choice
						id="{id}-library-artwork"
						name="artwork_language"
						value={library?.artwork_language ?? defaults.artwork_language}
						options={[
							{ value: "localized", label: "In its language first" },
							{ value: "any", label: "The most liked, any language" },
						]}
					/>
					<Field.Description>
						In its language first takes posters and logos in it, else English,
						else with no words on them.
					</Field.Description>
				</Field.Field>
				<Field.Field>
					<Field.Label for="{id}-library-country"
						>Certification country</Field.Label
					>
					<Choice
						id="{id}-library-country"
						name="certification_country"
						value={library?.certification_country ?? serverLocale}
						options={countries}
					/>
					<Field.Description>
						Whose certificates its titles carry, and parental controls read them
						by. Automatic is its language's country, else the server's.
					</Field.Description>
				</Field.Field>
				{#if !remote}
					<Field.Field>
						<Field.Label for="{id}-library-deletion"
							>Media deletion</Field.Label
						>
						<Choice
							id="{id}-library-deletion"
							name="deletion"
							value={library?.deletion ?? defaults.deletion}
							options={[
								{ value: "off", label: "Not allowed" },
								{ value: "files", label: "Allowed, files and all" },
							]}
						/>
						<Field.Description>
							Allowed, an admin's Delete removes a title's files from the disk,
							which the server must be able to write to. They cannot be brought
							back.
						</Field.Description>
					</Field.Field>
					<Field.Field>
						<Field.Label for="{id}-library-missing"
							>Versions on no disk</Field.Label
						>
						<Choice
							id="{id}-library-missing"
							name="missing_versions"
							value={library?.missing_versions ?? defaults.missing_versions}
							options={[
								{ value: "keep", label: "Kept, marked missing" },
								{ value: "forget", label: "Forgotten at the end of a scan" },
							]}
						/>
						<Field.Description>
							Kept, a version whose files have gone stays, marked missing, so a
							mount down for repair costs no titles; forget it instead and each
							scan takes it, and the titles left with nothing, out of the
							library.
						</Field.Description>
					</Field.Field>
				{/if}
			</div>
		</Field.Group>
	</details>
</Field.Group>
