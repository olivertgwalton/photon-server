import { createContext } from "svelte";
import type { components } from "#lib/api/schema.js";

// What the API's values are called, in the reader's language, fetched once for
// every signed-in page.
export const [vocabulary, setVocabulary] =
	createContext<components["schemas"]["Vocabulary"]>();
