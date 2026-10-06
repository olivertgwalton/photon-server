<script lang="ts">
import GaugeIcon from "@lucide/svelte/icons/gauge";
import LogOutIcon from "@lucide/svelte/icons/log-out";
import MonitorSmartphoneIcon from "@lucide/svelte/icons/monitor-smartphone";
import SettingsIcon from "@lucide/svelte/icons/settings";
import UsersIcon from "@lucide/svelte/icons/users";
import { goto } from "$app/navigation";
import { page } from "$app/state";
import type { components } from "#lib/api/schema.js";
import { logOut } from "#lib/logout.js";
import * as DropdownMenu from "#lib/components/ui/dropdown-menu/index.js";
import ProfileAvatar from "./ProfileAvatar.svelte";

let { profile }: { profile: components["schemas"]["Profile"] } = $props();

const here = $derived(encodeURIComponent(page.url.pathname + page.url.search));
</script>

<DropdownMenu.Root>
	<DropdownMenu.Trigger
		class="group rounded-full outline-none"
		aria-label="{profile.name}'s profile"
	>
		<ProfileAvatar
			name={profile.name}
			class="size-9 rounded-full after:rounded-full **:data-[slot=avatar-fallback]:rounded-full group-hover:**:data-[slot=avatar-fallback]:bg-ink group-hover:**:data-[slot=avatar-fallback]:text-ground"
		/>
	</DropdownMenu.Trigger>
	<DropdownMenu.Content align="end" class="w-56">
		<DropdownMenu.Label class="text-ink font-semibold">
			{profile.name}
		</DropdownMenu.Label>
		<DropdownMenu.Separator />
		<DropdownMenu.Item onSelect={() => goto(`/profiles?to=${here}`)}>
			<UsersIcon />Switch profile
		</DropdownMenu.Item>
		<DropdownMenu.Item onSelect={() => goto("/settings")}>
			<SettingsIcon />Settings
		</DropdownMenu.Item>
		<DropdownMenu.Item onSelect={() => goto("/settings/link")}>
			<MonitorSmartphoneIcon />Link a device
		</DropdownMenu.Item>
		{#if profile.role === "admin"}
			<DropdownMenu.Item onSelect={() => goto("/settings/server")}>
				<GaugeIcon />Server dashboard
			</DropdownMenu.Item>
		{/if}
		<DropdownMenu.Separator />
		<DropdownMenu.Item onSelect={logOut}>
			<LogOutIcon />Log out
		</DropdownMenu.Item>
	</DropdownMenu.Content>
</DropdownMenu.Root>
