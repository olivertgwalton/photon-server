<script lang="ts">
import ActivityIcon from "@lucide/svelte/icons/activity";
import ArchiveIcon from "@lucide/svelte/icons/archive";
import CalendarClockIcon from "@lucide/svelte/icons/calendar-clock";
import ChartLineIcon from "@lucide/svelte/icons/chart-line";
import DatabaseIcon from "@lucide/svelte/icons/database";
import GaugeIcon from "@lucide/svelte/icons/gauge";
import HardDriveIcon from "@lucide/svelte/icons/hard-drive";
import ImportIcon from "@lucide/svelte/icons/import";
import RotateCcwClockIcon from "@lucide/svelte/icons/rotate-ccw-clock";
import HouseIcon from "@lucide/svelte/icons/house";
import KeyRoundIcon from "@lucide/svelte/icons/key-round";
import LayersIcon from "@lucide/svelte/icons/layers";
import LibraryIcon from "@lucide/svelte/icons/library";
import LinkIcon from "@lucide/svelte/icons/link";
import LogInIcon from "@lucide/svelte/icons/log-in";
import ListChecksIcon from "@lucide/svelte/icons/list-checks";
import MonitorSmartphoneIcon from "@lucide/svelte/icons/monitor-smartphone";
import NetworkIcon from "@lucide/svelte/icons/network";
import SettingsIcon from "@lucide/svelte/icons/settings";
import ShieldCheckIcon from "@lucide/svelte/icons/shield-check";
import SlidersHorizontalIcon from "@lucide/svelte/icons/sliders-horizontal";
import TvIcon from "@lucide/svelte/icons/tv";
import UserIcon from "@lucide/svelte/icons/user";
import UsersIcon from "@lucide/svelte/icons/users";
import WebhookIcon from "@lucide/svelte/icons/webhook";
import type { Component } from "svelte";
import { page } from "$app/state";

let { data, children } = $props();

// One place, as Plex Web's settings are: what is the reader's for everyone,
// then the server for an admin.
const you: [string, string, Component][] = [
	["/settings", "Profile", UserIcon],
	["/settings/playback", "Playback", SlidersHorizontalIcon],
	["/settings/home", "Home", HouseIcon],
	["/settings/sign-in", "Sign-in", LogInIcon],
	["/settings/devices", "Devices", MonitorSmartphoneIcon],
	["/settings/link", "Link a device", TvIcon],
	["/settings/trackers", "Trackers", LinkIcon],
];
const server: [string, string, Component][] = [
	["/settings/server", "Dashboard", GaugeIcon],
	["/settings/server/general", "General", SettingsIcon],
	["/settings/server/metrics", "Metrics", ChartLineIcon],
	["/settings/server/libraries", "Libraries", LibraryIcon],
	["/settings/profiles", "Profiles", UsersIcon],
	["/settings/server/sign-in", "Sign-in", ShieldCheckIcon],
	["/settings/server/providers", "Metadata", DatabaseIcon],
	["/settings/server/network", "Network", NetworkIcon],
	["/settings/server/storage", "Storage", HardDriveIcon],
	["/settings/server/collections", "Collections", LayersIcon],
	["/settings/server/tasks", "Scheduled tasks", CalendarClockIcon],
	["/settings/server/jobs", "Jobs", ListChecksIcon],
	["/settings/server/activity", "Activity", ActivityIcon],
	["/settings/server/history", "Play history", RotateCcwClockIcon],
	["/settings/server/imports", "Import history", ImportIcon],
	["/settings/server/trackers", "Trackers", LinkIcon],
	["/settings/server/webhooks", "Webhooks", WebhookIcon],
	["/settings/server/backups", "Backups", ArchiveIcon],
	["/settings/server/keys", "API keys", KeyRoundIcon],
];
// A manager keeps profiles, and nothing else of the server's.
const manages: [string, string, Component][] = [
	["/settings/profiles", "Profiles", UsersIcon],
];
const groups = $derived(
	data.me.role === "admin"
		? [
				["Your account", you],
				["Server", server],
			]
		: data.me.role === "manager"
			? [
					["Your account", you],
					["Profiles", manages],
				]
			: [["Your account", you]],
) as [string, [string, string, Component][]][];

// On a phone the links are a strip: the page's own is brought into view.
let strip = $state<HTMLElement>();
$effect(() => {
	void page.url.pathname;
	strip
		?.querySelector("[aria-current=page]")
		?.scrollIntoView({ block: "nearest", inline: "center" });
});

function current(href: string) {
	const path = page.url.pathname;
	return href === "/settings" || href === "/settings/server"
		? path === href
		: path === href || path.startsWith(`${href}/`);
}
</script>

<div
	class="mx-auto grid max-w-7xl gap-6 lg:grid-cols-[13rem_minmax(0,1fr)] lg:gap-10"
>
	<nav aria-label="Settings" class="min-w-0 lg:sticky lg:top-20 lg:self-start">
		<!-- A strip that scrolls sideways on a phone, a column beside the page wider. -->
		<div
			bind:this={strip}
			class="-mx-3 flex gap-4 overflow-x-auto px-3 pb-1 sm:-mx-6 sm:px-6 lg:mx-0 lg:grid lg:gap-6 lg:overflow-visible lg:px-0"
		>
			{#each groups as [name, links] (name)}
				<div class="flex shrink-0 items-center gap-1 lg:grid lg:items-stretch">
					<p class="label hidden px-3 pb-1 lg:block">{name}</p>
					<ul class="flex gap-1 lg:grid">
						{#each links as [href, label, Icon] (href)}
							<li>
								<a
									{href}
									aria-current={current(href) ? "page" : undefined}
									class={[
										"flex items-center gap-2.5 rounded-full px-3.5 py-1.5 text-sm font-semibold whitespace-nowrap transition-colors duration-200 lg:rounded-lg",
										current(href)
											? "bg-raise text-ink"
											: "text-ink-3 hover:bg-raise/60 hover:text-ink",
									]}
								>
									<Icon
										class="hidden size-4 shrink-0 lg:block"
										aria-hidden="true"
									/>
									{label}
								</a>
							</li>
						{/each}
					</ul>
				</div>
			{/each}
		</div>
	</nav>
	<div class="grid min-w-0 content-start gap-6">{@render children()}</div>
</div>
