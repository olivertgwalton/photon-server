<script lang="ts">
import { act } from "#lib/act.js";
import { client } from "#lib/api/client.js";
import type { components } from "#lib/api/schema.js";
import { blurStyle } from "#lib/blurhash.js";
import Artwork from "#lib/components/Artwork.svelte";
import Rail from "#lib/components/Rail.svelte";
import { Button } from "#lib/components/ui/button/index.js";
import * as Dialog from "#lib/components/ui/dialog/index.js";

type Card = components["schemas"]["Card"];

// The titles a search found that the server does not hold, which its owner
// lets people ask for. Each opens to its picture and words and a way to ask
// for it: none is a title of the server's to open as one.
let { cards, server }: { cards: Card[]; server: string } = $props();

let open = $state(false);
let chosen = $state<Card>();
let asking = $state(false);

function choose(card: Card) {
	chosen = card;
	open = true;
}

async function ask() {
	if (!chosen) return;
	asking = true;
	const asked = await act(
		client().POST("/api/v1/requests", { body: { title_id: chosen.id } }),
		`Asked for ${chosen.title}.`,
	);
	asking = false;
	if (asked) open = false;
}
</script>

<Rail title="Not on {server} yet" items={cards}>
	{#snippet card(
		c: Card,
		sizes: string,
	)}
		<button
			type="button"
			class="group block w-full text-left outline-none"
			onclick={() => choose(c)}
		>
			<div
				class="card-frame aspect-2/3"
				style={c.poster ? blurStyle(c.blurhashes?.[c.poster]) : undefined}
			>
				{#if c.poster}
					<Artwork id={c.poster} shape="poster" {sizes} class="card-picture" />
				{/if}
				<div class="card-shade grid gap-0.5">
					<p
						class="text-ink line-clamp-2 text-[0.8125rem] leading-tight font-semibold"
					>
						{c.title}
					</p>
					{#if c.year}
						<p class="text-ink-2 text-xs">{c.year}</p>
					{/if}
				</div>
			</div>
		</button>
	{/snippet}
</Rail>

<Dialog.Root bind:open>
	<Dialog.Content class="sm:max-w-lg">
		{#if chosen}
			<div class="grid grid-cols-[6rem_1fr] gap-4">
				<div class="card-frame aspect-2/3">
					{#if chosen.poster}
						<Artwork
							id={chosen.poster}
							shape="poster"
							sizes="6rem"
							class="card-picture"
						/>
					{/if}
				</div>
				<Dialog.Header class="text-left">
					<Dialog.Title>{chosen.title}</Dialog.Title>
					<Dialog.Description>
						{[chosen.year, chosen.kind === "show" ? "Show" : "Film"]
							.filter(Boolean)
							.join(" · ")}
					</Dialog.Description>
					{#if chosen.overview}
						<p class="text-ink-2 line-clamp-6 text-sm">{chosen.overview}</p>
					{/if}
				</Dialog.Header>
			</div>
			<p class="text-ink-3 text-sm">
				It isn't on {server}. Ask for it, and you'll be told when it's added.
			</p>
			<Dialog.Footer>
				<Button disabled={asking} onclick={ask}>Ask for it</Button>
			</Dialog.Footer>
		{/if}
	</Dialog.Content>
</Dialog.Root>
