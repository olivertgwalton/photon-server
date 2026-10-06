<script lang="ts">
import ActivityIcon from "@lucide/svelte/icons/activity";
import type { components } from "#lib/api/schema.js";
import ScanProgress from "#lib/components/admin/ScanProgress.svelte";
import * as DropdownMenu from "#lib/components/ui/dropdown-menu/index.js";
import { live } from "#lib/live.svelte.js";

// What the server is doing, beside the profile, as Plex's activity menu is:
// drawn only while a library is being scanned.
let { libraries }: { libraries: components["schemas"]["Library"][] } = $props();

const scans = $derived(Object.values(live.scans));
const name = (id: string) =>
	libraries.find((l) => l.id === id)?.name ?? "A library";
</script>

{#if scans.length}
	<DropdownMenu.Root>
		<DropdownMenu.Trigger
			class="hover:bg-raise text-ink grid size-9 place-items-center rounded-full outline-none"
			aria-label="Activity: {scans.length === 1
				? `scanning ${name(scans[0].library_id)}`
				: `scanning ${scans.length} libraries`}"
		>
			<ActivityIcon class="size-5 motion-safe:animate-pulse" />
		</DropdownMenu.Trigger>
		<DropdownMenu.Content align="end" class="w-80">
			<DropdownMenu.Label class="text-ink font-semibold"
				>Activity</DropdownMenu.Label
			>
			<DropdownMenu.Separator />
			{#each scans as scan (scan.library_id)}
				<div class="grid gap-1 px-2 py-2" role="status">
					<p class="text-ink text-sm font-semibold">
						Scanning {name(scan.library_id)}
					</p>
					<ScanProgress {scan} name={name(scan.library_id)} />
				</div>
			{/each}
		</DropdownMenu.Content>
	</DropdownMenu.Root>
{/if}
