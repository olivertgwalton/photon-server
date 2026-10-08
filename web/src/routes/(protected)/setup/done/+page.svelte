<script lang="ts">
import { Button } from "#lib/components/ui/button/index.js";
import * as Card from "#lib/components/ui/card/index.js";

let { data } = $props();
</script>

<svelte:head><title>Set up · Photon</title></svelte:head>

<Card.Root class="w-full max-w-sm">
	<Card.Header>
		<Card.Title>
			<h1 class="heading text-xl">{data.server.name} is set up</h1>
		</Card.Title>
		<Card.Description>
			Its libraries are being scanned, and titles appear as they are matched.
			Add the rest of the household under Settings, Profiles.
		</Card.Description>
	</Card.Header>
	<Card.Content>
		<Button href="/" class="w-full">Start watching</Button>
	</Card.Content>
</Card.Root>
