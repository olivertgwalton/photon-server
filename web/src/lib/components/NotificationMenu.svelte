<script lang="ts">
import BellIcon from "@lucide/svelte/icons/bell";
import BellRingIcon from "@lucide/svelte/icons/bell-ring";
import { relative } from "#lib/admin/words.js";
import Artwork from "#lib/components/Artwork.svelte";
import { Badge } from "#lib/components/ui/badge/index.js";
import { Button } from "#lib/components/ui/button/index.js";
import * as Empty from "#lib/components/ui/empty/index.js";
import * as Item from "#lib/components/ui/item/index.js";
import * as Popover from "#lib/components/ui/popover/index.js";
import { ScrollArea } from "#lib/components/ui/scroll-area/index.js";
import { inbox, keepPushing } from "#lib/notifications.svelte.js";
import { afterNavigate, replaceState } from "$app/navigation";
import { page } from "$app/state";

// The films and episodes added that the profile follows, newest first, as
// Plex's and Jellyfin's apps keep a bell beside the profile.
$effect(() => {
	inbox.load();
	keepPushing();
});

// A notification is opened by a link naming it, from the bell or a push:
// the page cleans the link, and the notification is cleared.
afterNavigate(({ to }) => {
	const opened = Number(to?.url.searchParams.get("notification"));
	if (!to || !opened) return;
	inbox.clear(opened);
	const url = new URL(to.url.href);
	url.searchParams.delete("notification");
	replaceState(url, page.state);
});

let open = $state(false);
</script>

<Popover.Root bind:open>
	<Popover.Trigger
		class="hover:bg-raise text-ink relative grid size-9 place-items-center rounded-full outline-none"
		aria-label={inbox.unread
			? `Notifications: ${inbox.unread} unread`
			: "Notifications"}
	>
		{#if inbox.unread}
			<BellRingIcon class="size-5" />
			<Badge class="absolute -top-0.5 -right-0.5 h-4 min-w-4 px-1 tabular-nums">
				{inbox.unread > 9 ? "9+" : inbox.unread}
			</Badge>
		{:else}
			<BellIcon class="size-5" />
		{/if}
	</Popover.Trigger>
	<!-- The bell sits in a sticky header, so the page scrolling moves it in the
		document but not on screen. Positioned against the document the menu
		would need placing again every frame, and lag behind the scroll; against
		the viewport it needs placing only once. -->
	<Popover.Content
		align="end"
		class="grid w-88 max-w-[calc(100vw-1.5rem)] grid-cols-1 gap-1 p-2"
		strategy="fixed"
		role="dialog"
		aria-label="Notifications"
	>
		<div class="flex items-center justify-between gap-3 px-2">
			<Popover.Title class="text-ink font-semibold"
				>Notifications</Popover.Title
			>
			<div class="flex items-center">
				{#if inbox.unread}
					<Button variant="link" size="sm" onclick={() => inbox.readAll()}>
						Mark all read
					</Button>
				{/if}
				{#if inbox.notifications.length}
					<Button variant="link" size="sm" onclick={() => inbox.clearAll()}>
						Clear all
					</Button>
				{/if}
			</div>
		</div>
		{#if inbox.notifications.length}
			<ScrollArea class="max-h-[60vh]">
				<ul class="grid gap-1">
					{#each inbox.notifications as n (n.id)}
						<li>
							<Item.Root size="sm">
								{#snippet child({ props })}
									<a
										href="/titles/{n.title.id}?notification={n.id}"
										{...props}
										onclick={() => (open = false)}
									>
										<Item.Media
											variant="image"
											class="bg-raise aspect-2/3 h-auto w-10"
										>
											{#if n.title.poster}
												<Artwork
													id={n.title.poster}
													shape="poster"
													sizes="40px"
												/>
											{/if}
										</Item.Media>
										<Item.Content>
											<Item.Title class={n.read_at ? "text-ink-2" : ""}>
												{n.title.title}
											</Item.Title>
											<Item.Description>{n.message}</Item.Description>
											<span class="text-ink-3 text-xs">
												{relative(n.added_at, Date.now())}
											</span>
										</Item.Content>
										{#if !n.read_at}
											<Item.Actions>
												<span class="bg-signal size-2 rounded-full"></span>
												<span class="sr-only">Unread</span>
											</Item.Actions>
										{/if}
									</a>
								{/snippet}
							</Item.Root>
						</li>
					{/each}
				</ul>
			</ScrollArea>
		{:else}
			<Empty.Root class="p-6">
				<Empty.Header>
					<Empty.Media variant="icon"><BellIcon /></Empty.Media>
					<Empty.Title>Nothing new</Empty.Title>
					<Empty.Description>
						New episodes of the shows you follow arrive here.
					</Empty.Description>
				</Empty.Header>
			</Empty.Root>
		{/if}
		<Button
			variant="ghost"
			size="sm"
			href="/settings/notifications"
			class="justify-start"
			onclick={() => (open = false)}
		>
			Notification settings
		</Button>
	</Popover.Content>
</Popover.Root>
