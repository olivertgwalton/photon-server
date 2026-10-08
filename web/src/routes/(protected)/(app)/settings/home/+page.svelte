<script lang="ts">
import ArrowDownIcon from "@lucide/svelte/icons/arrow-down";
import ArrowUpIcon from "@lucide/svelte/icons/arrow-up";
import GripIcon from "@lucide/svelte/icons/grip-vertical";
import { tick } from "svelte";
import { toast } from "svelte-sonner";
import { client } from "#lib/api/client.js";
import { problemMessage } from "#lib/api/problem.js";
import type { components } from "#lib/api/schema.js";
import PageHeader from "#lib/components/PageHeader.svelte";
import { Button } from "#lib/components/ui/button/index.js";
import * as Card from "#lib/components/ui/card/index.js";
import { Switch } from "#lib/components/ui/switch/index.js";
import { moved } from "#lib/rows.js";
import { vocabulary } from "#lib/vocabulary.js";

type Section = components["schemas"]["HomeSection"];

let { data } = $props();
const words = vocabulary();
// Each change answers the home as kept; a load gives it afresh.
let kept = $state<Section[]>();
const home = $derived(kept ?? data.prefs.home);
// Said to a screen reader as a row moves, since the list redraws under focus.
let said = $state("");
let dragging = $state<number>();

async function save(next: Section[], message: string) {
	const before = kept;
	kept = next;
	const { data: saved, error } = await client().PATCH(
		"/api/v1/me/preferences",
		{ body: { home: next } },
	);
	if (error) {
		kept = before;
		toast.error(problemMessage(error));
		return;
	}
	kept = saved.home;
	said = message;
}

function move(from: number, to: number) {
	if (to < 0 || to >= home.length || to === from) return;
	const name = words.rows[home[from].row];
	void save(
		moved(home, from, to),
		`${name} moved to ${to + 1} of ${home.length}.`,
	);
}

function show(i: number, shown: boolean) {
	const name = words.rows[home[i].row];
	void save(
		home.with(i, { ...home[i], visibility: shown ? "shown" : "hidden" }),
		`${name} ${shown ? "shown" : "hidden"}.`,
	);
}

// A keyed row is moved in the page, which takes focus from its arrow: it is
// given back, or to the other arrow where this one has reached the end.
async function step(i: number, by: number) {
	const name = words.rows[home[i].row];
	move(i, i + by);
	await tick();
	const arrow = (way: string) =>
		document.querySelector<HTMLButtonElement>(
			`[aria-label="Move ${name} ${way}"]`,
		);
	const [ahead, back] = by < 0 ? ["up", "down"] : ["down", "up"];
	const target = arrow(ahead);
	(target && !target.disabled ? target : arrow(back))?.focus();
}
</script>

<PageHeader
	title="Home"
	description="The rows on your home page, on every device, in the order you put them. Drag a row, or move it with its arrows; a hidden row is not drawn."
/>

<Card.Root class="max-w-2xl">
	<Card.Content>
		<ol class="grid gap-2" aria-label="Home rows">
			{#each home as section, i (section.row)}
				{@const name = words.rows[section.row]}
				<li
					draggable="true"
					ondragstart={(e) => {
						dragging = i;
						e.dataTransfer?.setData("text/plain", section.row);
					}}
					ondragover={(e) => e.preventDefault()}
					ondrop={(e) => {
						e.preventDefault();
						if (dragging !== undefined) move(dragging, i);
						dragging = undefined;
					}}
					ondragend={() => (dragging = undefined)}
					class={[
						"bg-ground border-line flex items-center gap-3 rounded-lg border px-3 py-2",
						dragging === i && "opacity-50",
					]}
				>
					<GripIcon
						class="text-ink-3 size-4 shrink-0 cursor-grab"
						aria-hidden="true"
					/>
					<span
						class={[
							"min-w-0 flex-1 truncate text-sm font-semibold",
							section.visibility === "hidden" && "text-ink-3",
						]}
					>
						{name}
					</span>
					<Button
						variant="ghost"
						size="icon"
						aria-label="Move {name} up"
						disabled={i === 0}
						onclick={() => step(i, -1)}
					>
						<ArrowUpIcon />
					</Button>
					<Button
						variant="ghost"
						size="icon"
						aria-label="Move {name} down"
						disabled={i === home.length - 1}
						onclick={() => step(i, 1)}
					>
						<ArrowDownIcon />
					</Button>
					<Switch
						aria-label="Show {name}"
						checked={section.visibility === "shown"}
						onCheckedChange={(v) => show(i, v)}
					/>
				</li>
			{/each}
		</ol>
		<p class="sr-only" aria-live="polite">{said}</p>
	</Card.Content>
</Card.Root>
