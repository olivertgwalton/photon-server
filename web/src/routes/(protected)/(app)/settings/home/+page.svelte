<script lang="ts">
import ArrowDownIcon from "@lucide/svelte/icons/arrow-down";
import ArrowUpIcon from "@lucide/svelte/icons/arrow-up";
import GripIcon from "@lucide/svelte/icons/grip-vertical";
import type { Attachment } from "svelte/attachments";
import { toast } from "svelte-sonner";
import { client } from "#lib/api/client.js";
import { problemMessage } from "#lib/api/problem.js";
import type { components } from "#lib/api/schema.js";
import Choice from "#lib/components/Choice.svelte";
import PageHeader from "#lib/components/PageHeader.svelte";
import { Button } from "#lib/components/ui/button/index.js";
import * as Card from "#lib/components/ui/card/index.js";
import * as Field from "#lib/components/ui/field/index.js";
import { Switch } from "#lib/components/ui/switch/index.js";
import { reorder } from "#lib/reorder.svelte.js";
import { moved } from "#lib/rows.js";
import { vocabulary } from "#lib/vocabulary.js";

type Section = components["schemas"]["HomeSection"];
type CardArt = components["schemas"]["CardArt"];

let { data } = $props();
const words = vocabulary();
// Each change answers the home as kept, until a load gives it afresh.
let home = $derived(data.prefs.home);
let art = $derived(data.prefs.card_art);
// Said to a screen reader as a row moves, since the list redraws under focus.
let said = $state("");

// What the server last kept, to go back to where a save fails.
let kept = $derived(data.prefs.home);
let keptArt = $derived(data.prefs.card_art);
// Each save is sent once the one before is answered, so the server keeps the
// last order drawn; only the answer to that last one is drawn.
let sending = Promise.resolve();

function save(next: Section[], message: string) {
	home = next;
	const send = async () => {
		const { data: saved, error } = await client().PATCH(
			"/api/v1/profile/preferences",
			{
				body: {
					home: next.map(({ row, visibility }) => ({ row, visibility })),
				},
			},
		);
		if (saved) kept = saved.home;
		if (home !== next) return;
		if (error) {
			home = kept;
			toast.error(problemMessage(error));
			return;
		}
		home = saved.home;
		said = message;
	};
	sending = sending.then(send, send);
}

function move(from: number, to: number) {
	if (to < 0 || to >= home.length || to === from) return;
	const name = words.rows[home[from].row];
	save(moved(home, from, to), `${name} moved to ${to + 1} of ${home.length}.`);
}

function show(i: number, shown: boolean) {
	const name = words.rows[home[i].row];
	save(
		home.with(i, { ...home[i], visibility: shown ? "shown" : "hidden" }),
		`${name} ${shown ? "shown" : "hidden"}.`,
	);
}

const drag = reorder(move);

// What the rows about where the reader is draw a card by: the server answers
// those rows' cards with the picture chosen, so nothing here draws it twice.
const artOptions: { value: CardArt; label: string }[] = [
	{ value: "still", label: "The episode's still" },
	{ value: "backdrop", label: "The show's backdrop" },
];

// On the same queue as the order: a change is sent once the one before is
// answered, since the server keeps a change against the preferences as it
// reads them, and two at once would each drop the other's.
function picture(chosen: CardArt) {
	art = chosen;
	const send = async () => {
		const { data: saved, error } = await client().PATCH(
			"/api/v1/profile/preferences",
			{ body: { card_art: chosen } },
		);
		if (saved) keptArt = saved.card_art;
		if (art !== chosen) return;
		if (error) {
			art = keptArt;
			toast.error(problemMessage(error));
			return;
		}
		art = saved.card_art;
		toast.success("Saved for every device.");
	};
	sending = sending.then(send, send);
}

type Way = "up" | "down";

// A keyed row is moved in the page, which takes focus from its arrow: it is
// given back, or to the other arrow where this one has reached the end.
let focus = $state.raw<{ row: Section["row"]; way: Way }>();

function step(i: number, by: number) {
	const row = home[i].row;
	const to = i + by;
	move(i, to);
	if (by < 0) focus = { row, way: to === 0 ? "down" : "up" };
	else focus = { row, way: to === home.length - 1 ? "up" : "down" };
}

function arrow(row: Section["row"], way: Way): Attachment<HTMLElement> {
	return (button) => {
		if (focus?.row !== row || focus.way !== way) return;
		button.focus();
		focus = undefined;
	};
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
					{@attach drag.row(i)}
					class={[
						"bg-ground border-line flex items-center gap-3 rounded-lg border px-3 py-2",
						drag.dragging === i && "opacity-50",
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
						{@attach arrow(section.row, "up")}
					>
						<ArrowUpIcon />
					</Button>
					<Button
						variant="ghost"
						size="icon"
						aria-label="Move {name} down"
						disabled={i === home.length - 1}
						onclick={() => step(i, 1)}
						{@attach arrow(section.row, "down")}
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

<Card.Root class="mt-6 max-w-2xl">
	<Card.Content>
		<Field.Field>
			<Field.Label for="card-art">Row pictures</Field.Label>
			<Choice
				id="card-art"
				options={artOptions}
				value={art}
				onchange={picture}
				class="w-full sm:w-72"
			/>
			<Field.Description>
				What Continue Watching and Next Up draw a card by.
			</Field.Description>
		</Field.Field>
	</Card.Content>
</Card.Root>
