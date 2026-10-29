<script lang="ts">
	/** Connection fields of a custom S3 destination. */
	import type { S3Config } from '$lib/api/admin';
	import { t } from '$lib/i18n';

	let {
		s3 = $bindable(),
		secretSet = false,
		showPrefix = true,
		prefixLabel = '',
		prefixPlaceholder = '',
		prefixHint = ''
	}: {
		s3: S3Config;
		/** A secret is stored on the server; blank keeps it. */
		secretSet?: boolean;
		showPrefix?: boolean;
		prefixLabel?: string;
		prefixPlaceholder?: string;
		prefixHint?: string;
	} = $props();

	const input = 'mt-1 w-full px-3 py-2 bg-background border border-border rounded-lg text-sm focus:outline-none focus:ring-2 focus:ring-primary/40';
</script>

<div class="grid gap-3 sm:grid-cols-2">
	<label class="block text-sm"><span class="text-muted-foreground text-xs">{$t('s3.bucket')}</span><input bind:value={s3.bucket} autocomplete="off" class={input} /></label>
	<label class="block text-sm"><span class="text-muted-foreground text-xs">{$t('s3.region')}</span><input bind:value={s3.region} autocomplete="off" placeholder="us-east-1" class={input} /></label>
	<label class="block text-sm sm:col-span-2"><span class="text-muted-foreground text-xs">{$t('s3.endpoint')}</span><input bind:value={s3.endpoint} autocomplete="off" placeholder="https://s3.example.com" class={input} /></label>
	<label class="block text-sm"><span class="text-muted-foreground text-xs">{$t('s3.accessKey')}</span><input bind:value={s3.access_key} autocomplete="off" class="{input} font-mono" /></label>
	<label class="block text-sm"><span class="text-muted-foreground text-xs">{$t('s3.secret')}</span><input type="password" bind:value={s3.secret} autocomplete="new-password" placeholder={secretSet ? $t('s3.secretKeep') : ''} class="{input} font-mono" /></label>
	{#if showPrefix}
		<label class="block text-sm">
			<span class="text-muted-foreground text-xs">{prefixLabel || $t('s3.prefix')}</span>
			<input bind:value={s3.prefix} autocomplete="off" placeholder={prefixPlaceholder} class="{input} font-mono" />
			{#if prefixHint}<span class="mt-1 block text-[11px] text-muted-foreground break-all">{prefixHint}</span>{/if}
		</label>
	{/if}
	<label class="flex items-center gap-2 text-sm {showPrefix ? 'sm:mt-6 self-start' : 'sm:col-span-2'}"><input type="checkbox" bind:checked={s3.force_path_style} class="rounded border-border" />{$t('s3.pathStyle')}</label>
</div>
