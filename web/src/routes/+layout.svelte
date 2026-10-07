<script lang="ts">
import "../app.css";
import { onNavigate } from "$app/navigation";
import { Toaster } from "#lib/components/ui/sonner/index.js";

let { children } = $props();

// A page of the shell is led in from another by a View Transition, where the
// browser has them and the reader has not asked for less motion. Not the
// player, nor signing in: the pointer is the transition's while it runs.
const inShell = (id: string | null | undefined) =>
	!!id?.startsWith("/(protected)/(app)");

onNavigate((navigation) => {
	if (
		!inShell(navigation.from?.route.id) ||
		!inShell(navigation.to?.route.id) ||
		!document.startViewTransition ||
		matchMedia("(prefers-reduced-motion: reduce)").matches
	)
		return;
	return new Promise((resolve) => {
		document.startViewTransition(async () => {
			resolve();
			await navigation.complete;
		});
	});
});
</script>

<Toaster />

{@render children()}
