<script lang="ts">
import ActivityIcon from "@lucide/svelte/icons/activity";
import ArchiveIcon from "@lucide/svelte/icons/archive";
import BellIcon from "@lucide/svelte/icons/bell";
import CalendarClockIcon from "@lucide/svelte/icons/calendar-clock";
import ChartColumnIcon from "@lucide/svelte/icons/chart-column";
import ChartLineIcon from "@lucide/svelte/icons/chart-line";
import DatabaseIcon from "@lucide/svelte/icons/database";
import GaugeIcon from "@lucide/svelte/icons/gauge";
import HardDriveIcon from "@lucide/svelte/icons/hard-drive";
import HouseIcon from "@lucide/svelte/icons/house";
import ImportIcon from "@lucide/svelte/icons/import";
import KeyRoundIcon from "@lucide/svelte/icons/key-round";
import LayersIcon from "@lucide/svelte/icons/layers";
import LibraryIcon from "@lucide/svelte/icons/library";
import LinkIcon from "@lucide/svelte/icons/link";
import ListChecksIcon from "@lucide/svelte/icons/list-checks";
import LogInIcon from "@lucide/svelte/icons/log-in";
import MonitorSmartphoneIcon from "@lucide/svelte/icons/monitor-smartphone";
import NetworkIcon from "@lucide/svelte/icons/network";
import RotateCcwClockIcon from "@lucide/svelte/icons/rotate-ccw-clock";
import SettingsIcon from "@lucide/svelte/icons/settings";
import ShieldCheckIcon from "@lucide/svelte/icons/shield-check";
import SlidersHorizontalIcon from "@lucide/svelte/icons/sliders-horizontal";
import TvIcon from "@lucide/svelte/icons/tv";
import UserIcon from "@lucide/svelte/icons/user";
import UsersIcon from "@lucide/svelte/icons/users";
import WebhookIcon from "@lucide/svelte/icons/webhook";
import type { Component } from "svelte";
import { Input } from "#lib/components/ui/input/index.js";
import { settings } from "#lib/settings.js";
import { afterNavigate, goto } from "$app/navigation";
import { page } from "$app/state";

let { data, children } = $props();

// One place, as Plex Web's settings are: what is the reader's for everyone,
// then the server for an admin.
const you: [string, string, Component, string][] = [
	["/settings", "Profile", UserIcon, "avatar picture"],
	["/settings/playback", "Playback", SlidersHorizontalIcon, ""],
	["/settings/home", "Home", HouseIcon, "rows order hide"],
	[
		"/settings/notifications",
		"Notifications",
		BellIcon,
		"new episodes push alerts bell",
	],
	["/settings/sign-in", "Sign-in", LogInIcon, "link account provider oidc"],
	[
		"/settings/devices",
		"Devices",
		MonitorSmartphoneIcon,
		"sessions sign out signed in",
	],
	["/settings/link", "Link a device", TvIcon, "code tv pair"],
	[
		"/settings/trackers",
		"Trackers",
		LinkIcon,
		"trakt simkl anilist scrobble link account",
	],
];
const server: [string, string, Component, string][] = [
	[
		"/settings/server",
		"Dashboard",
		GaugeIcon,
		"now playing transcoding activity nodes scan all libraries",
	],
	["/settings/server/general", "General", SettingsIcon, ""],
	[
		"/settings/server/metrics",
		"Metrics",
		ChartLineIcon,
		"nodes streams transcodes bandwidth load jobs queue",
	],
	[
		"/settings/server/libraries",
		"Libraries",
		LibraryIcon,
		"folders films movies shows scan refresh add library",
	],
	[
		"/settings/profiles",
		"Profiles",
		UsersIcon,
		"users household role password access add profile",
	],
	[
		"/settings/server/sign-in",
		"Sign-in",
		ShieldCheckIcon,
		"openid connect oidc sso authelia authentik keycloak pocket id issuer client secret group",
	],
	[
		"/settings/server/providers",
		"Metadata",
		DatabaseIcon,
		"providers tmdb tvdb imdb plugins match",
	],
	["/settings/server/network", "Network", NetworkIcon, ""],
	["/settings/server/storage", "Storage", HardDriveIcon, "move"],
	[
		"/settings/server/collections",
		"Collections",
		LayersIcon,
		"box sets smart tmdb mdblist lists",
	],
	["/settings/server/tasks", "Scheduled tasks", CalendarClockIcon, "run now"],
	[
		"/settings/server/jobs",
		"Jobs",
		ListChecksIcon,
		"queue failed given up retry",
	],
	[
		"/settings/server/activity",
		"Activity",
		ActivityIcon,
		"events log sign-ins plays",
	],
	[
		"/settings/server/history",
		"Play history",
		RotateCcwClockIcon,
		"plays watched",
	],
	[
		"/settings/server/statistics",
		"Watch statistics",
		ChartColumnIcon,
		"stats hours most watched top titles who watched report",
	],
	[
		"/settings/server/imports",
		"Import history",
		ImportIcon,
		"plex jellyfin emby watch history watched migrate",
	],
	[
		"/settings/server/trackers",
		"Trackers",
		LinkIcon,
		"trakt simkl anilist client id",
	],
	[
		"/settings/server/webhooks",
		"Webhooks",
		WebhookIcon,
		"notify events deliveries",
	],
	[
		"/settings/server/backups",
		"Backups",
		ArchiveIcon,
		"database dumps restore",
	],
	["/settings/server/keys", "API keys", KeyRoundIcon, "tokens scripts revoke"],
];
// A manager keeps profiles, and nothing else of the server's.
const manages: [string, string, Component, string][] = [
	[
		"/settings/profiles",
		"Profiles",
		UsersIcon,
		"users household role password access add profile",
	],
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
) as [string, [string, string, Component, string][]][];

// A page is found by its name, and a setting by its own or its page's; every
// word asked matches one or the other.
let query = $state("");
const found = $derived.by(() => {
	const words = query.toLowerCase().split(/\s+/).filter(Boolean);
	const has = (text: string) =>
		words.every((w) => text.toLowerCase().includes(w));
	return groups
		.flatMap(([, links]) => links)
		.flatMap(([href, label, , terms]) => [
			...(has(`${label} ${terms}`) ? [{ href, name: label, page: "" }] : []),
			...(settings[href] ?? [])
				.filter(([, name, also]) => has(`${label} ${name} ${also}`))
				.map(([id, name]) => ({ href: `${href}#${id}`, name, page: label })),
		]);
});

function first(event: SubmitEvent) {
	event.preventDefault();
	if (found[0]) goto(found[0].href);
}

// On a phone the links are a strip: the page's own is brought into view.
let strip = $state<HTMLElement>();
afterNavigate(({ to }) => {
	strip
		?.querySelector("[aria-current=page]")
		?.scrollIntoView({ block: "nearest", inline: "center" });
	// A search result opens its page at the setting, and points it out.
	const id = to?.url.hash.slice(1);
	const setting = id && document.getElementById(decodeURIComponent(id));
	if (!setting) return;
	setting.scrollIntoView({ block: "center" });
	setting.focus({ preventScroll: true });
	(setting.closest("[data-slot=field]") ?? setting).animate(
		[{ backgroundColor: "var(--raise)" }, {}],
		{ duration: 1600, easing: "ease-out" },
	);
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
	<nav
		aria-label="Settings"
		class="min-w-0 lg:sticky lg:top-20 lg:max-h-[calc(100dvh-6rem)] lg:self-start lg:overflow-y-auto"
	>
		<search class="mb-4">
			<form onsubmit={first}>
				<Input
					type="search"
					bind:value={query}
					placeholder="Search settings"
					aria-label="Search settings"
				/>
			</form>
		</search>
		{#if query.trim()}
			<ul class="grid gap-1">
				{#each found as { href, name, page: on } (href)}
					<li>
						<a
							{href}
							class="text-ink hover:bg-raise/60 block rounded-lg px-3.5 py-1.5 text-sm font-semibold"
						>
							{name}
							{#if on}
								<span class="text-ink-3 font-normal">· {on}</span>
							{/if}
						</a>
					</li>
				{:else}
					<li class="text-ink-3 px-3 text-sm">No settings match.</li>
				{/each}
			</ul>
		{:else}
			<!-- A strip that scrolls sideways on a phone, a column beside the page wider. -->
			<div
				bind:this={strip}
				class="-mx-3 flex gap-4 overflow-x-auto px-3 pb-1 sm:-mx-6 sm:px-6 lg:mx-0 lg:grid lg:gap-6 lg:overflow-visible lg:px-0"
			>
				{#each groups as [name, links] (name)}
					<div
						class="flex shrink-0 items-center gap-1 lg:grid lg:items-stretch"
					>
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
		{/if}
	</nav>
	<div class="grid min-w-0 content-start gap-6">{@render children()}</div>
</div>
