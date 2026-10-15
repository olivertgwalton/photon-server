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
import { inbox } from "#lib/notifications.svelte.js";

// The films and episodes added that the profile follows, newest first, as
// Plex's and Jellyfin's apps keep a bell beside the profile.
$effect(() => {
	inbox.load();
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
	<Popover.Content
		align="end"
		class="grid w-88 max-w-[calc(100vw-1.5rem)] grid-cols-1 gap-1 p-2"
		role="dialog"
		aria-label="Notifications"
	>
		<div class="flex items-center justify-between gap-3 px-2">
			<Popover.Title class="text-ink font-semibold"
				>Notifications</Popover.Title
			>
			{#if inbox.unread}
				<Button variant="link" size="sm" onclick={() => inbox.readAll()}>
					Mark all read
				</Button>
			{/if}
		</div>
		{#if inbox.notifications.length}
			<ScrollArea class="max-h-[60vh]">
				<ul class="grid gap-1">
					{#each inbox.notifications as n (n.id)}
						<li>
							<Item.Root size="sm">
								{#snippet child({
									props,
								})}
									<a
										href="/titles/{n.title.id}"
										{...props}
										onclick={() => {
											inbox.read(n);
											open = false;
										}}
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
