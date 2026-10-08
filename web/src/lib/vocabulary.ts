import { createContext } from "svelte";
import type { components } from "#lib/api/schema.js";

// What the API's values are called, in the reader's language, fetched once by
// the shell for every page under it.
export const [vocabulary, setVocabulary] =
	createContext<components["schemas"]["Vocabulary"]>();
