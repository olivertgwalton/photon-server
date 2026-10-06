<script lang="ts">
import type { components } from "#lib/api/schema.js";
import { extraKinds } from "#lib/admin/words.js";
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
}: {
	library?: Schemas["AdminLibrary"];
	providers: Schemas["MetadataProvider"][];
} = $props();

const defaults = {
	sources: ["nfo", "tmdb"],
	remote_extras: ["trailer", "featurette", "behind_the_scenes"],
	monitor: "realtime",
	refresh_days: 30,
	previews: "all",
	markers: "all",
	keyframes: "index",
} as const;

// A new library's kind changes the sources offered; a library's own is fixed.
const kindOf = () => library?.kind ?? "movies";
let kind = $state<Schemas["LibraryKind"]>(kindOf());

const offered = $derived([
	{ id: "nfo" as const, name: "NFO files beside the media", ready: true },
	...providers
		.filter((p) => p.kinds.includes(kind === "movies" ? "movie" : "show"))
		.map((p) => ({ id: String(p.id), name: p.name, ready: p.ready })),
]);

const extras = $derived(library?.remote_extras ?? defaults.remote_extras);

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

	<Field.Set>
		<Field.Legend>Metadata, most trusted first</Field.Legend>
		<Field.Description>
			Drag a source, or move it with its arrows. Changing these identifies every
			title in the library again.
		</Field.Description>
		{#key kind}
			<SourceRanker
				name="sources"
				{offered}
				chosen={(library?.sources ?? defaults.sources).map(String)}
			/>
		{/key}
	</Field.Set>

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
					{ value: "all", label: "From chapters and by sound" },
					{ value: "chapters", label: "From chapters only" },
					{ value: "off", label: "Not looked for" },
				]}
			/>
			<Field.Description>
				Comparing sound reads the start and end of every episode.
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
				through gives exact segments from a local disk.
			</Field.Description>
		</Field.Field>
	</div>
</Field.Group>
