<script lang="ts">
import { untrack } from "svelte";
import { setVocabulary } from "#lib/vocabulary.js";

let { data, children } = $props();

setVocabulary(untrack(() => data.words));
</script>

{@render children()}
