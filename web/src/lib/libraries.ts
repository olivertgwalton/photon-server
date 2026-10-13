import ClapperboardIcon from "@lucide/svelte/icons/clapperboard";
import FilmIcon from "@lucide/svelte/icons/film";
import TvIcon from "@lucide/svelte/icons/tv";
import type { Component } from "svelte";
import type { components } from "#lib/api/schema.js";

// The picture beside a library's name, for what it holds.
export const libraryIcons: Record<
	components["schemas"]["LibraryKind"],
	Component
> = {
	movies: FilmIcon,
	shows: TvIcon,
	mixed: ClapperboardIcon,
};
