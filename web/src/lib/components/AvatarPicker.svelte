<script lang="ts">
import { act } from "#lib/act.js";
import { Button } from "#lib/components/ui/button/index.js";

// Gives a profile its picture, or takes it away, at path: the profile's own
// address or an admin's for any profile. The picture goes up as it is; the
// server says whether it is one it keeps.
let {
	name,
	avatar,
	path,
}: { name: string; avatar?: string | null; path: string } = $props();

let input = $state<HTMLInputElement>();

async function send(method: "POST" | "DELETE", body?: File) {
	const answer = await fetch(path, { method, body });
	return answer.ok ? {} : { error: await answer.json().catch(() => ({})) };
}

async function chosen() {
	const file = input?.files?.[0];
	if (!file || !input) return;
	await act(send("POST", file), `${name}'s picture was saved.`);
	input.value = "";
}
</script>

<div class="flex flex-wrap gap-2">
	<Button variant="outline" size="sm" onclick={() => input?.click()}>
		{avatar ? "Change picture" : "Add a picture"}
	</Button>
	<input
		bind:this={input}
		type="file"
		accept="image/jpeg,image/png,image/gif,image/webp"
		class="sr-only"
		tabindex="-1"
		aria-label="Picture for {name}"
		onchange={chosen}
	>
	{#if avatar}
		<Button
			variant="ghost"
			size="sm"
			onclick={() => act(send("DELETE"), `${name}'s picture was removed.`)}
		>
			Remove picture
		</Button>
	{/if}
</div>
