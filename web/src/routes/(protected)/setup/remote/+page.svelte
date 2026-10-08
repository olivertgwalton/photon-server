<script lang="ts">
import { act } from "#lib/act.js";
import { client } from "#lib/api/client.js";
import { Button } from "#lib/components/ui/button/index.js";
import * as Card from "#lib/components/ui/card/index.js";
import * as Field from "#lib/components/ui/field/index.js";
import { Input } from "#lib/components/ui/input/index.js";
import { fields } from "#lib/form.js";

let { data } = $props();

// How the server is reached from away, as Plex's and Jellyfin's wizards ask:
// the rest of the network is left as it is.
function save(event: SubmitEvent) {
	const form = fields(event);
	const { jellyfin_error: _, ...network } = data.network;
	return act(
		client().PUT("/api/v1/admin/network", {
			body: {
				...network,
				public_url: String(form.get("public_url") ?? "").trim(),
				remote_max_bitrate_kbps: Math.round(
					Number(form.get("remote_max_mbps")) * 1000,
				),
			},
		}),
		undefined,
		"/setup/done",
	);
}
</script>

<svelte:head><title>Remote access · Set up · Photon</title></svelte:head>

<Card.Root class="w-full max-w-2xl">
	<Card.Header>
		<Card.Title
			><h1 class="heading text-xl">Watching away from home</h1></Card.Title
		>
		<Card.Description>
			Apps away from home reach the server wherever its port is forwarded or a
			proxy serves it. Both can be left for later, under Settings, Network.
		</Card.Description>
	</Card.Header>
	<Card.Content>
		<form onsubmit={save} class="grid gap-6">
			<Field.Field>
				<Field.Label for="public-url">Public address</Field.Label>
				<Input
					id="public-url"
					name="public_url"
					type="url"
					value={data.network.public_url}
					placeholder="https://photon.example.com"
					class="font-mono"
				/>
				<Field.Description>
					Where people reach it from outside, as a TV's sign-in link names it.
					Left empty, each device is answered at the address it used.
				</Field.Description>
			</Field.Field>
			<Field.Field>
				<Field.Label for="remote-max"
					>Remote streaming limit (Mbps)</Field.Label
				>
				<Input
					id="remote-max"
					name="remote_max_mbps"
					type="number"
					min={0}
					step={0.5}
					required
					value={data.network.remote_max_bitrate_kbps / 1000}
					class="w-32 font-mono"
				/>
				<Field.Description>
					The most a stream away from home is sent at, so it leaves the upload
					room. 0 is no limit.
				</Field.Description>
			</Field.Field>
			<div class="flex flex-wrap gap-2">
				<Button type="submit">Next</Button>
				<Button href="/setup/done" variant="outline">Skip for now</Button>
			</div>
		</form>
	</Card.Content>
</Card.Root>
