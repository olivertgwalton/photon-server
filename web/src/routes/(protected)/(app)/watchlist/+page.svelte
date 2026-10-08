<script lang="ts">
import RowWall from "#lib/components/RowWall.svelte";

let { data } = $props();
</script>

<RowWall kind={data.kind} page={data.page} />
