<script lang="ts">
import "../app.css";
import { MediaQuery } from "svelte/reactivity";
import { Toaster } from "#lib/components/ui/sonner/index.ts";
import { onNavigate } from "$app/navigation";

let { children } = $props();

const reducedMotion = new MediaQuery("prefers-reduced-motion: reduce");

// A page of the shell is led in from another by a View Transition, where the
// browser has them and the reader has not asked for less motion. Not the
// player, nor signing in: the pointer is the transition's while it runs. Nor
// the same page with its query changed, a filter or a sort: the transition
// paints the page over an open menu while it runs. Nor from a page scrolled
// down it: Safari draws the shell a scroll too high while the transition runs,
// so the header is gone until it ends.
const inShell = (id: string | null | undefined) =>
	!!id?.startsWith("/(protected)/(app)");

onNavigate((navigation) => {
	if (
		!inShell(navigation.from?.route.id) ||
		!inShell(navigation.to?.route.id) ||
		navigation.from?.url.pathname === navigation.to?.url.pathname ||
		window.scrollY > 0 ||
		!document.startViewTransition ||
		reducedMotion.current
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
