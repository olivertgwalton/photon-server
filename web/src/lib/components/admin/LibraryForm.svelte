<script lang="ts">
import type { components } from "#lib/api/schema.js";
import {
	defaultSources,
	itemKinds,
	localeOptions,
	offeredSources,
	serverLocale,
} from "#lib/admin/library.js";
import { extraKinds } from "#lib/admin/words.js";
import { language } from "#lib/format.js";
import { subtitleLanguages } from "#lib/player/words.js";
import { Checkbox } from "#lib/components/ui/checkbox/index.js";
import * as Field from "#lib/components/ui/field/index.js";
import { Input } from "#lib/components/ui/input/index.js";
import { Label } from "#lib/components/ui/label/index.js";
import Choice from "./Choice.svelte";
import FolderPicker from "./FolderPicker.svelte";
import SourceRanker from "./SourceRanker.svelte";

type Schemas = components["schemas"];

// What a library is and how it is kept: the fields of a new library and of
// one being changed. A library's kind and folder are fixed once it is made.
let {
	library,
	providers,
	locales,
	serverLanguage,
}: {
	library?: Schemas["AdminLibrary"];
	providers: Schemas["MetadataProvider"][];
	locales: Schemas["Locales"];
	// The server's own metadata language, which a library asks in by default.
	serverLanguage: string;
} = $props();

// Languages and countries by name in the reader's own language, as Plex's and
// Jellyfin's library settings list them.
const languageNames = new Intl.DisplayNames(undefined, { type: "language" });
const countryNames = new Intl.DisplayNames(undefined, { type: "region" });
const serverCountry = $derived(new Intl.Locale(serverLanguage).region);
const languages = $derived([
	{
		value: serverLocale,
		label: `Server default (${languageNames.of(serverLanguage) ?? serverLanguage})`,
	},
	...localeOptions(locales.languages, "language"),
]);
const countries = $derived([
	{
		value: serverLocale,
		label: `Automatic${serverCountry ? ` (${countryNames.of(serverCountry)})` : ""}`,
	},
	...localeOptions(locales.countries, "region"),
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
	artwork_language: "localized",
	title_language: "localized",
	collection_mode: "grouped",
	subtitle_match: "release",
} as const;

// A new library's kind changes the sources offered; a library's own is fixed.
const kindOf = () => library?.kind ?? "movies";
let kind = $state<Schemas["LibraryKind"]>(kindOf());

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

const chosen = (item: Schemas["ItemKind"]) =>
	library?.sources.find((s) => s.kind === item) ?? defaultSources(item);

const extras = $derived(library?.remote_extras ?? defaults.remote_extras);

// The languages offered: the common ones, and any the library already has.
const fetching = $derived(library?.subtitle_languages ?? []);
const offeredLanguages = $derived(
	[...new Set([...subtitleLanguages, ...fetching])]
		.map((tag) => ({ tag, name: language(tag) }))
		.sort((a, b) => a.name.localeCompare(b.name)),
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
		<Field.Label for="library-name">Name</Field.Label>
		<Input id="library-name" name="name" required value={library?.name ?? ""} />
	</Field.Field>

	{#if !library}
		<Field.Field>
			<Field.Label for="library-kind">Holds</Field.Label>
			<Choice
				id="library-kind"
				name="kind"
				bind:value={kind}
				options={[
					{ value: "movies", label: "Films" },
					{ value: "shows", label: "Shows" },
				]}
			/>
		</Field.Field>
		<Field.Field>
			<Field.Label for="library-root">Folder</Field.Label>
			<FolderPicker id="library-root" name="root" />
			<Field.Description>
				A folder on the server. Everything under it is scanned.
			</Field.Description>
		</Field.Field>
	{:else}
		<Field.Field>
			<Field.Label for="library-root">Folder</Field.Label>
			<Input
				id="library-root"
				value={library.root}
				readonly
				class="font-mono"
			/>
		</Field.Field>
	{/if}

	{#key kind}
		{#each itemKinds(kind) as [item, items] (item)}
			{#each fetchers as { f, title, said } (f)}
				{@const id = `sources-${item}-${f}`}
				<Field.Set>
					<Field.Legend id="{id}-legend">{title} ({items})</Field.Legend>
					<Field.Description>{said}</Field.Description>
					<SourceRanker
						name={id}
						labelledby="{id}-legend"
						offered={offeredSources(providers, f, item)}
						chosen={chosen(item)[f]}
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
			{#each Object.entries(extraKinds) as [value, label] (value)}
				<div class="flex items-center gap-2">
					<Checkbox
						id="extra-{value}"
						name="remote_extras"
						{value}
						checked={(extras as string[]).includes(value)}
					/>
					<Label for="extra-{value}">{label}</Label>
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
			{#each offeredLanguages as { tag, name } (tag)}
				<div class="flex items-center gap-2">
					<Checkbox
						id="subtitle-{tag}"
						name="subtitle_languages"
						value={tag}
						checked={fetching.includes(tag)}
					/>
					<Label for="subtitle-{tag}">{name}</Label>
				</div>
			{/each}
		</div>
		<Field.Field>
			<Field.Label for="library-subtitle-match">Download</Field.Label>
			<Choice
				id="library-subtitle-match"
				name="subtitle_match"
				value={library?.subtitle_match ?? defaults.subtitle_match}
				options={[
					{ value: "release", label: "Only those made for the file" },
					{ value: "any", label: "The best found" },
				]}
			/>
			<Field.Description>
				One made for another release of the title may be out of time with this
				one.
			</Field.Description>
		</Field.Field>
	</Field.Set>

	<div class="grid gap-4 sm:grid-cols-2">
		<Field.Field>
			<Field.Label for="library-monitor">Watch for changes</Field.Label>
			<Choice
				id="library-monitor"
				name="monitor"
				value={library?.monitor ?? defaults.monitor}
				options={[
					{ value: "realtime", label: "As they happen" },
					{ value: "off", label: "Only when scanned" },
				]}
			/>
		</Field.Field>
		<Field.Field>
			<Field.Label for="library-refresh">Refresh metadata</Field.Label>
			<Choice
				id="library-refresh"
				name="refresh_days"
				value={refresh}
				options={refreshOptions}
			/>
		</Field.Field>
		<Field.Field>
			<Field.Label for="library-previews">Previews</Field.Label>
			<Choice
				id="library-previews"
				name="previews"
				value={library?.previews ?? defaults.previews}
				options={[
					{ value: "all", label: "Chapters and seeking" },
					{ value: "chapters", label: "Chapter images only" },
					{ value: "off", label: "None" },
				]}
			/>
		</Field.Field>
		<Field.Field>
			<Field.Label for="library-markers">Intros and credits</Field.Label>
			<Choice
				id="library-markers"
				name="markers"
				value={library?.markers ?? defaults.markers}
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
			<Field.Label for="library-keyframes">Keyframes</Field.Label>
			<Choice
				id="library-keyframes"
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
				through, in the maintenance window, gives exact segments from a local
				disk.
			</Field.Description>
		</Field.Field>
		<Field.Field>
			<Field.Label for="library-themes">Theme music</Field.Label>
			<Choice
				id="library-themes"
				name="themes"
				value={library?.themes ?? defaults.themes}
				options={[
					{ value: "local", label: "Local files only" },
					{ value: "themerr", label: "Local files and ThemerrDB's" },
					{ value: "off", label: "None" },
				]}
			/>
			<Field.Description>
				A theme.mp3 or a theme-music folder beside a title. With ThemerrDB's, a
				film or show with neither downloads the theme audio from the YouTube
				link ThemerrDB lists for it, which needs yt-dlp on the server.
			</Field.Description>
		</Field.Field>
		<Field.Field>
			<Field.Label for="library-language">Metadata language</Field.Label>
			<Choice
				id="library-language"
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
			<Field.Label for="library-titles">Titles</Field.Label>
			<Choice
				id="library-titles"
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
			<Field.Label for="library-collections">Collections</Field.Label>
			<Choice
				id="library-collections"
				name="collection_mode"
				value={library?.collection_mode ?? defaults.collection_mode}
				options={[
					{ value: "grouped", label: "In place of their titles" },
					{ value: "shown", label: "Beside their titles" },
					{ value: "hidden", label: "Hidden" },
				]}
			/>
			<Field.Description>
				How the library shows its collections among its titles. A filtered
				library shows its titles alone.
			</Field.Description>
		</Field.Field>
		<Field.Field>
			<Field.Label for="library-artwork">Pictures</Field.Label>
			<Choice
				id="library-artwork"
				name="artwork_language"
				value={library?.artwork_language ?? defaults.artwork_language}
				options={[
					{ value: "localized", label: "In its language first" },
					{ value: "any", label: "The most liked, any language" },
				]}
			/>
			<Field.Description>
				In its language first takes posters and logos in it, else English, else
				with no words on them.
			</Field.Description>
		</Field.Field>
		<Field.Field>
			<Field.Label for="library-country">Certification country</Field.Label>
			<Choice
				id="library-country"
				name="certification_country"
				value={library?.certification_country ?? serverLocale}
				options={countries}
			/>
			<Field.Description>
				Whose certificates its titles carry, and parental controls read them by.
				Automatic is its language's country, else the server's.
			</Field.Description>
		</Field.Field>
		<Field.Field>
			<Field.Label for="library-deletion">Media deletion</Field.Label>
			<Choice
				id="library-deletion"
				name="deletion"
				value={library?.deletion ?? defaults.deletion}
				options={[
					{ value: "off", label: "Not allowed" },
					{ value: "files", label: "Allowed, files and all" },
				]}
			/>
			<Field.Description>
				Allowed, an admin's Delete removes a title's files from the disk, which
				the server must be able to write to. They cannot be brought back.
			</Field.Description>
		</Field.Field>
	</div>
</Field.Group>
