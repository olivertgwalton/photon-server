<script lang="ts">
import BellOffIcon from "@lucide/svelte/icons/bell-off";
import { toast } from "svelte-sonner";
import { client } from "#lib/api/client.js";
import { problemMessage } from "#lib/api/problem.js";
import type { components } from "#lib/api/schema.js";
import Choice from "#lib/components/Choice.svelte";
import PageHeader from "#lib/components/PageHeader.svelte";
import * as Alert from "#lib/components/ui/alert/index.js";
import * as Card from "#lib/components/ui/card/index.js";
import * as Field from "#lib/components/ui/field/index.js";
import { Switch } from "#lib/components/ui/switch/index.js";

type Notify = components["schemas"]["Notify"];

let { data } = $props();
let notify = $derived(data.prefs.notify);
let subscriptions = $derived(data.subscriptions);

const notifyOptions: { value: Notify; label: string }[] = [
	{ value: "marked", label: "New episodes of shows I follow" },
	{ value: "all", label: "Every new film and episode" },
	{ value: "none", label: "Nothing" },
];

async function setNotify(v: Notify) {
	const { data: kept, error } = await client().PATCH(
		"/api/v1/profile/preferences",
		{ body: { notify: v } },
	);
	if (error) {
		toast.error(problemMessage(error));
		return;
	}
	notify = kept.notify;
	toast.success("Saved for every device.");
}

// Whether this browser is pushed the profile's notifications. A browser
// without push, or outside a secure page, cannot be; one whose person blocked
// notifications is asked to allow them in its own settings.
type PushState = "unsupported" | "denied" | "off" | "on";

const supported =
	typeof window !== "undefined" &&
	window.isSecureContext &&
	"serviceWorker" in navigator &&
	"PushManager" in window;

let browser = $state<PushSubscription | null>(null);
let busy = $state(false);

$effect(() => {
	if (!supported) return;
	navigator.serviceWorker.ready
		.then((reg) => reg.pushManager.getSubscription())
		.then((sub) => (browser = sub));
});

const pushState: PushState = $derived.by(() => {
	if (!supported) return "unsupported";
	if (Notification.permission === "denied") return "denied";
	return browser && subscriptions.some((s) => s.endpoint === browser?.endpoint)
		? "on"
		: "off";
});

async function subscribe() {
	if ((await Notification.requestPermission()) !== "granted") {
		toast.error("Notifications are blocked for this site in your browser.");
		return;
	}
	const reg = await navigator.serviceWorker.ready;
	const sub =
		(await reg.pushManager.getSubscription()) ??
		(await reg.pushManager.subscribe({
			userVisibleOnly: true,
			applicationServerKey: Uint8Array.fromBase64(data.key, {
				alphabet: "base64url",
			}),
		}));
	const { data: made, error } = await client().POST(
		"/api/v1/profile/push-subscriptions",
		{ body: sub.toJSON() as components["schemas"]["Subscribe"] },
	);
	if (error) {
		toast.error(problemMessage(error));
		return;
	}
	browser = sub;
	subscriptions = [...subscriptions.filter((s) => s.id !== made.id), made];
	toast.success("This browser will be told.");
}

async function unsubscribe() {
	const sub = browser;
	const kept = subscriptions.find((s) => s.endpoint === sub?.endpoint);
	if (kept) {
		const { error } = await client().DELETE(
			"/api/v1/profile/push-subscriptions/{id}",
			{ params: { path: { id: kept.id } } },
		);
		if (error) {
			toast.error(problemMessage(error));
			return;
		}
	}
	await sub?.unsubscribe();
	browser = null;
	subscriptions = subscriptions.filter((s) => s.id !== kept?.id);
	toast.success("This browser will no longer be told.");
}

async function setPush(on: boolean) {
	busy = true;
	try {
		await (on ? subscribe() : unsubscribe());
	} catch (e) {
		toast.error(e instanceof Error ? e.message : "Something went wrong.");
	} finally {
		busy = false;
	}
}

const others = $derived(
	subscriptions.filter((s) => s.endpoint !== browser?.endpoint).length,
);
</script>

<PageHeader
	title="Notifications"
	description="What you're told of as it's added to the server: in the bell at the top of every page, and on the browsers you choose."
/>

<Card.Root class="max-w-2xl">
	<Card.Content>
		<Field.Group>
			<Field.Field>
				<Field.Label for="notify">Tell me of</Field.Label>
				<Choice
					id="notify"
					name="notify"
					options={notifyOptions}
					value={notify}
					onchange={setNotify}
					class="w-full sm:w-72"
				/>
				<Field.Description>
					A show you follow is one on your watchlist, a favourite, or one you've
					started. Episodes before the furthest you've watched aren't news.
				</Field.Description>
			</Field.Field>
			<Field.Separator />
			<Field.Field orientation="horizontal">
				<Switch
					id="push"
					checked={pushState === "on"}
					disabled={busy ||
						pushState === "unsupported" ||
						pushState === "denied"}
					onCheckedChange={setPush}
				/>
				<Field.Content>
					<Field.Label for="push">Notify this browser</Field.Label>
					<Field.Description>
						Shown even when photon isn't open. Each browser is chosen on its
						own.
						{#if others}
							{others === 1
								? "1 other browser is notified."
								: `${others} other browsers are notified.`}
						{/if}
					</Field.Description>
				</Field.Content>
			</Field.Field>
			{#if pushState === "unsupported"}
				<Alert.Root>
					<BellOffIcon />
					<Alert.Title>This browser can't be notified here</Alert.Title>
					<Alert.Description>
						It needs a secure (https) address, and on an iPhone or iPad photon
						added to the Home Screen.
					</Alert.Description>
				</Alert.Root>
			{:else if pushState === "denied"}
				<Alert.Root variant="destructive">
					<BellOffIcon />
					<Alert.Title>Notifications are blocked for this site</Alert.Title>
					<Alert.Description>
						Allow them in your browser's site settings, then turn this on.
					</Alert.Description>
				</Alert.Root>
			{/if}
		</Field.Group>
	</Card.Content>
</Card.Root>
