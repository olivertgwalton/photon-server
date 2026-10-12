<script lang="ts">
import ServerSettingsForm from "#lib/components/admin/ServerSettingsForm.svelte";
import PageHeader from "#lib/components/PageHeader.svelte";

let { data } = $props();
</script>

<svelte:head><title>General · Settings · Photon</title></svelte:head>

<PageHeader
	title="General"
	description="What the server is called, and what it describes titles in."
/>

<ServerSettingsForm settings={data.server} locales={data.locales} />
