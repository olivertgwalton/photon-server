<script lang="ts">
import { act } from "#lib/act.js";
import { client } from "#lib/api/client.js";
import { problemMessage } from "#lib/api/problem.js";
import type { components } from "#lib/api/schema.js";
import { Badge } from "#lib/components/ui/badge/index.js";
import { Button } from "#lib/components/ui/button/index.js";
import * as Card from "#lib/components/ui/card/index.js";
import * as Dialog from "#lib/components/ui/dialog/index.js";
import { Input } from "#lib/components/ui/input/index.js";
import * as Tabs from "#lib/components/ui/tabs/index.js";

type KeptList = components["schemas"]["KeptList"];

// The lists TMDB and MDBList keep, read from them when asked, as Silo's
// collection templates: one chosen is made into a list collection found in
// every library.
let { libraries }: { libraries: string[] } = $props();

let open = $state(false);
let source = $state("tmdb");
let query = $state("");
// Asked only while the dialog is open, the only time its content is drawn.
const lists = $derived(
	client().GET("/api/v1/admin/lists", {
		params: { query: { source, query: query || undefined } },
	}),
);

function search(event: SubmitEvent) {
	event.preventDefault();
	query = String(
		new FormData(event.currentTarget as HTMLFormElement).get("query"),
	);
}

async function make(list: KeptList) {
	const made = await act(
		client().POST("/api/v1/admin/collections", {
			body: {
				title: list.title,
				list: { source: list.source, id: list.id },
				library_ids: libraries,
			},
		}),
		undefined,
		(made) => `/settings/server/collections/${made.id}`,
	);
	if (made) open = false;
}
</script>

<Button variant="outline" onclick={() => (open = true)}>Browse lists</Button>

<Dialog.Root bind:open>
	<!-- One height whatever it lists, so it does not jump as the lists change. -->
	<Dialog.Content class="flex h-[85svh] flex-col sm:max-w-4xl">
		<Dialog.Header>
			<Dialog.Title>Lists to follow</Dialog.Title>
			<Dialog.Description>
				TMDB's charts and MDBList's best liked lists. One made into a collection
				holds the titles of it your libraries have, read again daily.
			</Dialog.Description>
		</Dialog.Header>
		<Tabs.Root bind:value={source}>
			<Tabs.List>
				<Tabs.Trigger value="tmdb">TMDB</Tabs.Trigger>
				<Tabs.Trigger value="mdblist">MDBList</Tabs.Trigger>
			</Tabs.List>
		</Tabs.Root>
		<form onsubmit={search} role="search">
			<Input
				type="search"
				name="query"
				value={query}
				placeholder="Search lists"
				aria-label="Search lists"
			/>
		</form>
		<div class="-mx-6 min-h-0 flex-1 overflow-y-auto px-6 pb-1">
			<svelte:boundary>
				{@const answer = await lists}
				{#if answer.error}
					<p role="alert" class="text-destructive text-sm">
						{problemMessage(answer.error)}
					</p>
				{:else}
					<!-- The lists shown stay, dimmed, until the next come. -->
					<ul
						class={[
							"grid gap-2 transition-opacity sm:grid-cols-2 lg:grid-cols-3",
							$effect.pending() && "opacity-60",
						]}
						aria-label="Lists"
					>
						{#each answer.data.items as list (list.id)}
							<li>
								<button
									type="button"
									onclick={() => make(list)}
									class="h-full w-full rounded-xl text-left"
								>
									<Card.Root size="sm" class="hover:ring-foreground/30 h-full">
										<Card.Header>
											<Card.Title>{list.title}</Card.Title>
											{#if list.about}
												<Card.Description class="line-clamp-3"
													>{list.about}</Card.Description
												>
											{/if}
										</Card.Header>
										<Card.Content class="mt-auto flex gap-2">
											<Badge variant="outline"
												>{list.holds === "movie"
													? "Films"
													: list.holds === "show"
														? "Shows"
														: "Films and shows"}</Badge
											>
											{#if list.size}
												<Badge variant="secondary">{list.size} titles</Badge>
											{/if}
										</Card.Content>
									</Card.Root>
								</button>
							</li>
						{:else}
							<li class="text-muted-foreground text-sm">No list matches.</li>
						{/each}
					</ul>
				{/if}
				{#snippet pending()}
					<p class="text-muted-foreground text-sm">
						Asking {source === "tmdb" ? "TMDB" : "MDBList"}…
					</p>
				{/snippet}
			</svelte:boundary>
		</div>
	</Dialog.Content>
</Dialog.Root>
