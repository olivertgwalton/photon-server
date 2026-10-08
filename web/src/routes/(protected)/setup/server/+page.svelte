<script lang="ts">
import ServerSettingsForm from "#lib/components/admin/ServerSettingsForm.svelte";
import * as Card from "#lib/components/ui/card/index.js";

let { data } = $props();
</script>

<svelte:head><title>Name the server · Set up · Photon</title></svelte:head>

<Card.Root class="w-full max-w-2xl">
	<Card.Header>
		<Card.Title><h1 class="heading text-xl">Name the server</h1></Card.Title>
		<Card.Description>
			What apps call it, and what it describes titles in. Each can be changed
			later under Settings, General.
		</Card.Description>
	</Card.Header>
	<Card.Content>
		<ServerSettingsForm
			settings={data.server}
			locales={data.locales}
			submit="Next"
			to="/setup/libraries"
		/>
	</Card.Content>
</Card.Root>
