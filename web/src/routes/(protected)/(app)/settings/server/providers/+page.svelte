<script lang="ts">
import { vocabulary } from "#lib/vocabulary.js";
import PageHeader from "#lib/components/PageHeader.svelte";
import { act } from "#lib/act.js";
import { fields } from "#lib/form.js";
import { client } from "#lib/api/client.js";
import type { components } from "#lib/api/schema.js";
import ConfirmButton from "#lib/components/admin/ConfirmButton.svelte";
import Choice from "#lib/components/Choice.svelte";
import { Badge } from "#lib/components/ui/badge/index.js";
import { Button } from "#lib/components/ui/button/index.js";
import * as Card from "#lib/components/ui/card/index.js";
import * as Field from "#lib/components/ui/field/index.js";
import { Input } from "#lib/components/ui/input/index.js";

type Provider = components["schemas"]["MetadataProvider"];

let { data } = $props();
const words = vocabulary();

const api = client();

const capabilities = {
	describe: "Describes titles",
	search: "Searches, to fix a match",
	rate: "Ratings",
	person: "People",
	list: "Lists",
	stream: "Streams",
	subtitles: "Subtitles",
	events: "Events",
	segments: "Intros and credits",
} as const;

// A plugin's slug, from its source id plugin:<slug>.
function slug(provider: unknown) {
	return String(provider).replace(/^plugin:/, "");
}

function changeSettings(provider: Provider, settings: Record<string, string>) {
	return act(
		api.PATCH("/api/v1/admin/providers/{id}", {
			params: { path: { id: String(provider.id) } },
			body: { settings },
		}),
		`${provider.name} was saved.`,
	);
}

// Every setting typed in; a secret left empty keeps what was set, as it is
// never shown to edit.
async function save(event: SubmitEvent, provider: Provider) {
	const form = fields(event);
	const settings: Record<string, string> = {};
	for (const s of provider.settings) {
		const value = String(form.get(s.key) ?? "");
		if (s.secret && !value) continue;
		settings[s.key] = value;
	}
	if (await changeSettings(provider, settings)) {
		(event.currentTarget as HTMLFormElement).reset();
	}
}

// What a plugin speaks: photon's own protocol, or Stremio's, as an addon is
// installed by its manifest's address.
const protocols = [
	{ value: "photon", label: "Photon plugin" },
	{ value: "stremio", label: "Stremio addon" },
] as const;
let protocol = $state<(typeof protocols)[number]["value"]>("photon");

async function register(event: SubmitEvent) {
	const form = fields(event);
	const url = String(form.get("url") ?? "");
	const id = String(form.get("id") ?? "").trim() || undefined;
	const element = event.currentTarget as HTMLFormElement;
	if (
		await act(
			api.POST("/api/v1/admin/plugins", { body: { url, protocol, id } }),
			"The plugin was registered.",
		)
	) {
		element.reset();
	}
}
</script>

<PageHeader
	title="Metadata providers"
	description="Where titles are matched and described. A library chooses which of these it asks, and in what order, in its own settings."
/>

<div class="grid items-start gap-4 lg:grid-cols-2">
	{#each data.providers as provider (provider.id)}
		{@const id = String(provider.id)}
		<Card.Root>
			<Card.Header>
				<Card.Title><h2 class="heading">{provider.name}</h2></Card.Title>
				<Card.Description>
					{provider.kinds.map((k) => words.kinds[k] ?? k).join(" and ")}
				</Card.Description>
				<Card.Action>
					<Badge variant={provider.ready ? "secondary" : "destructive"}>
						{provider.ready ? "Ready" : "Needs settings"}
					</Badge>
				</Card.Action>
			</Card.Header>
			<Card.Content class="grid gap-4">
				<div class="flex flex-wrap gap-1.5">
					{#each provider.capabilities as capability (capability)}
						<Badge variant="outline">{capabilities[capability]}</Badge>
					{/each}
				</div>
				{#if provider.settings.length}
					<form onsubmit={(event) => save(event, provider)}>
						<Field.Group>
							{#each provider.settings as setting (setting.key)}
								<Field.Field>
									<Field.Label for="{id}-{setting.key}">
										{setting.name}{setting.required ? " (required)" : ""}
									</Field.Label>
									{#if setting.secret}
										<Input
											id="{id}-{setting.key}"
											name={setting.key}
											type="password"
											autocomplete="off"
											placeholder={setting.set ? "Set; type to replace it" : ""}
										/>
									{:else}
										<Input
											id="{id}-{setting.key}"
											name={setting.key}
											value={setting.value ?? ""}
										/>
									{/if}
								</Field.Field>
							{/each}
							<Field.Field orientation="horizontal">
								<Button type="submit">
									Save <span class="sr-only">{provider.name} settings</span>
								</Button>
								{#each provider.settings.filter(
									(s) => s.secret && s.set,
								) as setting (setting.key)}
									<Button
										variant="outline"
										onclick={() =>
											changeSettings(provider, { [setting.key]: "" })}
									>
										Clear {setting.name}
									</Button>
								{/each}
							</Field.Field>
						</Field.Group>
					</form>
				{/if}
			</Card.Content>
		</Card.Root>
	{/each}
</div>

<section aria-labelledby="plugins" class="grid gap-4">
	<h2 id="plugins" class="heading">Plugins</h2>
	<p class="max-w-2xl text-sm">
		A plugin is a web service that describes titles, registered by the address
		its manifest is under, or a Stremio addon, registered by its manifest's
		address as Stremio installs it, whose catalogs are lists. Once registered it
		is a provider like the ones above.
	</p>
	{#if data.plugins.length}
		<ul class="grid gap-3">
			{#each data.plugins as plugin (plugin.id)}
				{@const path = { params: { path: { slug: slug(plugin.provider) } } }}
				<li
					class="bg-raise flex flex-wrap items-center gap-x-4 gap-y-2 rounded-xl p-4"
				>
					<div class="grid min-w-0 flex-1 gap-1">
						<p class="text-ink font-semibold">{plugin.name}</p>
						<p class="text-ink-3 truncate font-mono text-xs">{plugin.url}</p>
						<p class="text-ink-3 text-xs">
							{plugin.protocol === "stremio"
								? "Stremio addon"
								: `Protocol ${plugin.version}`}
							·
							{plugin.kinds.map((k) => words.kinds[k] ?? k).join(" and ")}
							·
							{plugin.capabilities.map((c) => capabilities[c]).join(", ")}
						</p>
					</div>
					<Button
						variant="outline"
						size="sm"
						onclick={() =>
							act(
								api.POST("/api/v1/admin/plugins/{slug}/refresh", path),
								`${plugin.name}'s manifest was read again.`,
							)}
					>
						Read manifest again <span class="sr-only">of {plugin.name}</span>
					</Button>
					<ConfirmButton
						label="Remove"
						hidden={plugin.name}
						title="Remove {plugin.name}?"
						confirm="Remove plugin"
						onconfirm={() =>
							act(
								api.DELETE("/api/v1/admin/plugins/{slug}", path),
								`${plugin.name} was removed.`,
							)}
						body={"What it has said about titles stands until another source says otherwise."}
					/>
				</li>
			{/each}
		</ul>
	{/if}
	<form onsubmit={register} class="max-w-2xl">
		<Field.Field>
			<Field.Label for="plugin-url">Register a plugin</Field.Label>
			<div class="flex flex-wrap gap-2">
				<Choice
					aria-label="It speaks"
					bind:value={protocol}
					options={protocols}
					class="w-40"
				/>
				<Input
					id="plugin-url"
					name="url"
					type="url"
					required
					class="min-w-60 flex-1"
					placeholder={protocol === "stremio"
						? "https://addon.example/manifest.json"
						: "http://plugin:8080"}
				/>
				{#if protocol === "stremio"}
					<Input
						name="id"
						aria-label="Its id, where another install of it has its own"
						placeholder="id (optional)"
						class="w-40 font-mono"
					/>
				{/if}
				<Button type="submit">Register</Button>
			</div>
		</Field.Field>
	</form>
</section>
