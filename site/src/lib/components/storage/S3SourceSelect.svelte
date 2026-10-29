<script lang="ts" generics="T extends string">
	/**
	 * Storage picker: the system S3 an administrator manages, or a custom
	 * connection. The custom fields (children) stay folded away until
	 * "Custom" is chosen.
	 */
	import { untrack, type Snippet } from 'svelte';
	import { t } from '$lib/i18n';

	let {
		source = $bindable(),
		available,
		path = '',
		missingText,
		legacy,
		id,
		disabled = false,
		children
	}: {
		source: T;
		/** Whether the system S3 can be used here. */
		available: boolean;
		/** Where this use writes on the system S3, e.g. "diarum/audit/". */
		path?: string;
		/** Shown when the system S3 is chosen but not available. */
		missingText: string;
		/** An older source still selected by saved settings, offered only while selected. */
		legacy?: { value: string; label: string; hint: string };
		id?: string;
		disabled?: boolean;
		children?: Snippet;
	} = $props();

	// Offer the legacy option only when the saved settings use it, and keep it
	// while this form is open so switching away can be undone.
	const initial = untrack(() => source);
	let legacyOption = $derived(legacy && initial === legacy.value ? legacy : undefined);
</script>

<div class="space-y-2">
	<label for={id} class="block text-xs text-muted-foreground">{$t('s3.storage')}</label>
	<select
		{id}
		bind:value={source}
		{disabled}
		class="w-full sm:w-72 px-3 py-2 bg-background border border-border rounded-lg text-sm focus:outline-none focus:ring-2 focus:ring-primary/40 disabled:opacity-60"
	>
		<option value="system">{available ? $t('s3.system') : $t('s3.systemNotReady')}</option>
		{#if legacyOption}<option value={legacyOption.value}>{legacyOption.label}</option>{/if}
		<option value="custom">{$t('s3.custom')}</option>
	</select>

	{#if source === 'system'}
		{#if available}
			<p class="text-xs text-muted-foreground break-all">{$t('s3.systemPath', { path })}</p>
		{:else}
			<p class="text-xs text-amber-600 dark:text-amber-400">{missingText}</p>
		{/if}
	{:else if legacyOption && source === legacyOption.value}
		<p class="text-xs text-muted-foreground break-all">{legacyOption.hint}</p>
	{:else if source === 'custom' && children}
		<div class="pt-1 animate-fade-in">{@render children()}</div>
	{/if}
</div>
