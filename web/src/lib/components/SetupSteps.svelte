<script lang="ts">
// Where a new server's setup has got to, as Jellyfin's wizard shows it: the
// admin first, then what the server is called, its libraries, and how it is
// reached from away.
let { current }: { current: number } = $props();

const steps = ["Admin", "Server", "Libraries", "Remote access"];
</script>

<ol class="flex flex-wrap gap-x-4 gap-y-1 text-sm" aria-label="Setup">
	{#each steps as step, i (step)}
		<li
			aria-current={i === current ? "step" : undefined}
			class={i === current ? "text-ink font-medium" : "text-ink-3"}
		>
			<span class="font-mono">{i + 1}</span>
			{step}
		</li>
	{/each}
</ol>
