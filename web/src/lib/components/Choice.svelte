<script lang="ts" generics="T extends string">
import * as Select from "#lib/components/ui/select/index.ts";

// One of a few answers, sent with its form by `name` where it has one, and
// named by its label followed by the answer shown, as a native select reads.
let {
	options,
	value = $bindable(),
	onchange,
	name,
	id,
	class: className,
	"aria-label": ariaLabel,
	"aria-labelledby": labelledby,
}: {
	options: readonly { value: T; label: string; disabled?: boolean }[];
	value?: T;
	onchange?: (value: T) => void;
	name?: string;
	id?: string;
	class?: string;
	"aria-label"?: string;
	"aria-labelledby"?: string;
} = $props();

const uid = $props.id();
// A <label for={id}> names it natively; an ARIA name must carry the answer too.
const named = $derived(
	labelledby ?? (ariaLabel === undefined ? undefined : `${uid}-label`),
);
const label = $derived(options.find((o) => o.value === value)?.label);
</script>

<Select.Root
	type="single"
	{name}
	bind:value={value as string}
	onValueChange={(v) => onchange?.(v as T)}
>
	<Select.Trigger
		{id}
		class={className}
		aria-labelledby={named && `${named} ${uid}-value`}
	>
		{#if ariaLabel !== undefined && !labelledby}
			<span id="{uid}-label" class="sr-only">{ariaLabel}</span>
		{/if}
		<span id="{uid}-value" class="truncate">{label ?? "Choose"}</span>
	</Select.Trigger>
	<Select.Content>
		{#each options as option (option.value)}
			<Select.Item
				value={option.value}
				label={option.label}
				disabled={option.disabled}
			>
				{option.label}
			</Select.Item>
		{/each}
	</Select.Content>
</Select.Root>
