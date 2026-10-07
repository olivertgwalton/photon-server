<script lang="ts">
import PageHeader from "#lib/components/PageHeader.svelte";
import { act } from "#lib/admin/act.js";
import { client } from "#lib/api/client.js";
import type { components } from "#lib/api/schema.js";
import Choice from "#lib/components/admin/Choice.svelte";
import { Button } from "#lib/components/ui/button/index.js";
import * as Field from "#lib/components/ui/field/index.js";
import { Input } from "#lib/components/ui/input/index.js";
import { fields } from "#lib/form.js";

let { data } = $props();

type Secure = components["schemas"]["SecureConnections"];
type Jellyfin = components["schemas"]["JellyfinMode"];

// As Plex's Secure connections.
const modes: readonly { value: Secure; label: string }[] = [
	{ value: "required", label: "Required" },
	{ value: "preferred", label: "Preferred" },
	{ value: "disabled", label: "Disabled" },
];

const jellyfinModes: readonly { value: Jellyfin; label: string }[] = [
	{ value: "on", label: "On" },
	{ value: "off", label: "Off" },
];

// What is chosen, until a save loads the page again.
let secure = $derived<Secure>(data.network.secure_connections);
let jellyfin = $derived<Jellyfin>(data.network.jellyfin);

function save(event: SubmitEvent) {
	const form = fields(event);
	const path = (name: string) => String(form.get(name) ?? "").trim();
	return act(
		client().PUT("/api/v1/admin/network", {
			body: {
				secure_connections: secure,
				certificate: path("certificate"),
				key: path("key"),
				jellyfin,
				jellyfin_port: Number(form.get("jellyfin_port")),
			},
		}),
		"Saved. Every server serves it now.",
	);
}
</script>

<PageHeader
	title="Network"
	description="How the server is reached. Behind a reverse proxy that has the certificate, leave secure connections disabled."
/>

<form onsubmit={save} class="grid max-w-2xl gap-6">
	<Field.Set>
		<Field.Legend>Secure connections</Field.Legend>
		<Field.Description>
			Required sends a browser that comes over plain HTTP to HTTPS, but one on
			the server itself. Preferred answers both. Disabled answers plain HTTP
			alone.
		</Field.Description>
		<Field.Field>
			<Field.Label for="secure-connections">Secure connections</Field.Label>
			<Choice
				id="secure-connections"
				name="secure_connections"
				bind:value={secure}
				options={modes}
				class="w-48"
			/>
		</Field.Field>
	</Field.Set>
	<Field.Set>
		<Field.Legend>Certificate</Field.Legend>
		<Field.Description>
			A PEM certificate chain and its key, at paths on the server, as certbot,
			Caddy or Tailscale write them. A certificate renewed in place is served
			within a minute.
		</Field.Description>
		<Field.Group>
			<Field.Field>
				<Field.Label for="certificate">Certificate</Field.Label>
				<Input
					id="certificate"
					name="certificate"
					value={data.network.certificate ?? ""}
					required={secure !== "disabled"}
					placeholder="/certs/fullchain.pem"
					class="font-mono"
				/>
			</Field.Field>
			<Field.Field>
				<Field.Label for="key">Key</Field.Label>
				<Input
					id="key"
					name="key"
					value={data.network.key ?? ""}
					required={secure !== "disabled"}
					placeholder="/certs/privkey.pem"
					class="font-mono"
				/>
			</Field.Field>
		</Field.Group>
	</Field.Set>
	<Field.Set>
		<Field.Legend>Jellyfin apps</Field.Legend>
		<Field.Description>
			Lets apps made for Jellyfin, such as Infuse, Swiftfin and Findroid, sign
			in on a port of their own, with a profile's name and password. Every
			server listens on it; where one runs in a container, publish the port.
		</Field.Description>
		<Field.Group>
			<Field.Field>
				<Field.Label for="jellyfin">Jellyfin apps</Field.Label>
				<Choice
					id="jellyfin"
					name="jellyfin"
					bind:value={jellyfin}
					options={jellyfinModes}
					class="w-48"
				/>
			</Field.Field>
			<Field.Field>
				<Field.Label for="jellyfin-port">Port</Field.Label>
				<Input
					id="jellyfin-port"
					name="jellyfin_port"
					type="number"
					min={1}
					max={65535}
					required
					value={data.network.jellyfin_port}
					class="w-32 font-mono"
				/>
				<Field.Description>
					8096 is Jellyfin's own, where its apps look first.
				</Field.Description>
				<Field.Error errors={[{ message: data.network.jellyfin_error }]} />
			</Field.Field>
		</Field.Group>
	</Field.Set>
	<div><Button type="submit">Save</Button></div>
</form>
