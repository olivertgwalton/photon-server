<script lang="ts">
import PageHeader from "#lib/components/PageHeader.svelte";
import { act } from "#lib/act.js";
import { client } from "#lib/api/client.js";
import type { components } from "#lib/api/schema.js";
import { fields } from "#lib/form.js";
import { vocabulary } from "#lib/vocabulary.js";
import { Badge } from "#lib/components/ui/badge/index.js";
import { Button } from "#lib/components/ui/button/index.js";
import * as Card from "#lib/components/ui/card/index.js";
import * as Field from "#lib/components/ui/field/index.js";
import { Input } from "#lib/components/ui/input/index.js";

type Tracker = components["schemas"]["Tracker"];

let { data } = $props();
const words = vocabulary();

// Where an admin registers the server's app on each tracker, and what to
// register: the device flow needs a client id and nothing else.
const register: Record<Tracker, { href: string; how: string }> = {
	trakt: {
		href: "https://app.trakt.tv/settings/apps",
		how: "Create an app on Trakt and copy its client id.",
	},
	simkl: {
		href: "https://simkl.com/settings/developer/",
		how: "Register an AUTH V2 app on Simkl of the type “TV, devices & command line”, and copy its client id.",
	},
};

function save(event: SubmitEvent, tracker: Tracker) {
	const name = words.trackers[tracker] ?? tracker;
	const clientID = String(fields(event).get("client_id") ?? "").trim();
	return act(
		client().PUT("/api/v1/admin/trackers/{tracker}", {
			params: { path: { tracker } },
			body: { client_id: clientID },
		}),
		clientID ? `${name} was saved.` : `${name} can no longer be linked.`,
	);
}
</script>

<PageHeader
	title="Trackers"
	description="Services that keep what people watch. Once one is set up here, each profile links its own account on it in its settings."
/>

<div class="grid items-start gap-4 lg:grid-cols-2">
	{#each data.trackers as t (t.tracker)}
		{@const name = words.trackers[t.tracker] ?? t.tracker}
		<Card.Root>
			<Card.Header>
				<Card.Title><h2 class="heading">{name}</h2></Card.Title>
				<Card.Description>
					{register[t.tracker].how}
					<a
						class="text-ink underline"
						href={register[t.tracker].href}
						target="_blank"
						rel="noopener noreferrer"
						>Open {name}'s apps</a
					>
				</Card.Description>
				<Card.Action>
					<Badge variant={t.client_id ? "secondary" : "outline"}>
						{t.client_id ? "Set up" : "Not set up"}
					</Badge>
				</Card.Action>
			</Card.Header>
			<Card.Content>
				<form onsubmit={(event) => save(event, t.tracker)}>
					<Field.Group>
						<Field.Field>
							<Field.Label for="{t.tracker}-client-id">Client id</Field.Label>
							<Input
								id="{t.tracker}-client-id"
								name="client_id"
								autocomplete="off"
								spellcheck="false"
								class="font-mono"
								value={t.client_id}
							/>
							<Field.Description>
								Empty, no profile can link {name}; accounts already linked stay.
							</Field.Description>
						</Field.Field>
						<Field.Field orientation="horizontal">
							<Button type="submit">
								Save <span class="sr-only">{name}</span>
							</Button>
						</Field.Field>
					</Field.Group>
				</form>
			</Card.Content>
		</Card.Root>
	{/each}
</div>
