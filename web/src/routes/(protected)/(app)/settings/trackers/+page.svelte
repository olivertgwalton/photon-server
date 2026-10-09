<script lang="ts">
import { invalidate } from "$app/navigation";
import PageHeader from "#lib/components/PageHeader.svelte";
import ConfirmButton from "#lib/components/admin/ConfirmButton.svelte";
import { when } from "#lib/admin/words.js";
import { act } from "#lib/act.js";
import { client } from "#lib/api/client.js";
import type { components } from "#lib/api/schema.js";
import { keys } from "#lib/changes.js";
import { vocabulary } from "#lib/vocabulary.js";
import { Badge } from "#lib/components/ui/badge/index.js";
import { Button } from "#lib/components/ui/button/index.js";
import * as Card from "#lib/components/ui/card/index.js";

type Tracker = components["schemas"]["Tracker"];

let { data } = $props();
const words = vocabulary();
const api = client();

// What each tracker keeps.
const about: Record<Tracker, string> = {
	trakt: "Keeps what you watch, rate and want to watch, across apps.",
	simkl: "Keeps what you watch of films, shows and anime, across apps.",
	mdblist: "Keeps what you watch beside your lists and ratings, across apps.",
};

// When each code shown expires, by this browser's clock. The server asks the
// tracker after it and the page reloads once it is entered; a code left until
// it expires is taken away.
const deadlines = $derived(
	new Map(
		data.trackers.flatMap((t) =>
			t.code ? [[t.tracker, Date.now() + t.code.expires_in_ms] as const] : [],
		),
	),
);
let now = $state(Date.now());
$effect(() => {
	if (!deadlines.size) return;
	const tick = setInterval(() => {
		now = Date.now();
		if ([...deadlines.values()].some((d) => d <= now))
			invalidate(keys.trackers);
	}, 1000);
	return () => clearInterval(tick);
});

function left(tracker: Tracker) {
	const s = Math.max(
		0,
		Math.round(((deadlines.get(tracker) ?? now) - now) / 1000),
	);
	return `${Math.floor(s / 60)}:${String(s % 60).padStart(2, "0")}`;
}

const path = (tracker: Tracker) => ({ params: { path: { tracker } } });
const link = (tracker: Tracker) =>
	act(api.POST("/api/v1/profile/trackers/{tracker}/link", path(tracker)));
const unlink = (tracker: Tracker, said: string) =>
	act(api.DELETE("/api/v1/profile/trackers/{tracker}", path(tracker)), said);
</script>

<PageHeader
	title="Trackers"
	description="Link your accounts on the services that keep what you watch."
/>

<div class="grid items-start gap-4 lg:grid-cols-2">
	{#each data.trackers as t (t.tracker)}
		{@const name = words.trackers[t.tracker] ?? t.tracker}
		<Card.Root>
			<Card.Header>
				<Card.Title><h2 class="heading">{name}</h2></Card.Title>
				<Card.Description>{about[t.tracker]}</Card.Description>
				{#if t.state === "linked"}
					<Card.Action><Badge variant="secondary">Linked</Badge></Card.Action>
				{/if}
			</Card.Header>
			<Card.Content class="grid gap-4">
				{#if t.state === "unavailable"}
					<p class="text-ink-3 text-sm">
						{#if data.me.role === "admin"}
							{name}
							is not set up yet: give it the client id of your app on {name} in
							<a class="text-ink underline" href="/settings/server/trackers"
								>Trackers</a
							>.
						{:else}
							An admin sets {name} up before it can be linked.
						{/if}
					</p>
				{:else if t.state === "unlinked"}
					<div>
						<Button onclick={() => link(t.tracker)}>Link {name}</Button>
					</div>
				{:else if t.state === "linking" && t.code}
					<div class="grid gap-3">
						<p class="text-sm">
							Open {name} and allow this server, or enter this code at
							<a
								class="text-ink underline"
								href={t.code.verification_uri}
								target="_blank"
								rel="noopener noreferrer"
								>{t.code.verification_uri.replace(/^https:\/\//, "")}</a
							>:
						</p>
						<p class="text-ink font-mono text-3xl tracking-[0.2em]">
							{t.code.user_code}
						</p>
						<p class="text-ink-3 text-sm">Expires in {left(t.tracker)}</p>
						<div class="flex flex-wrap gap-2">
							<Button
								href={t.code.verification_uri_complete}
								target="_blank"
								rel="noopener noreferrer"
							>
								Open {name}
							</Button>
							<Button variant="outline" onclick={() => unlink(t.tracker, "")}>
								Cancel
							</Button>
						</div>
					</div>
				{:else if t.state === "linked"}
					<div class="flex flex-wrap items-center gap-x-4 gap-y-2">
						<p class="min-w-0 flex-1 text-sm">
							Linked as <span class="text-ink font-semibold">{t.username}</span>
							{#if t.linked_at}
								since {when.format(new Date(t.linked_at))}
							{/if}
						</p>
						<ConfirmButton
							label="Unlink"
							hidden={name}
							title="Unlink {name}?"
							body="{name} forgets this server's access. What {name} already keeps stays there."
							confirm="Unlink"
							onconfirm={() => unlink(t.tracker, `${name} is unlinked.`)}
						/>
					</div>
				{/if}
			</Card.Content>
		</Card.Root>
	{/each}
</div>
