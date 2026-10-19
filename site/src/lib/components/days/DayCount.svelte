<script lang="ts">
	import type { DayCount } from '$lib/utils/days';
	import { t, locale } from '$lib/i18n';

	/** The "还有 12 天" / "已经 340 天" figure used by list rows and home tiles. */
	let { count, size = 'md' }: { count: DayCount; size?: 'sm' | 'md' } = $props();

	let intl = $derived($locale === 'zh' ? 'zh-CN' : 'en-US');
	let tone = $derived(
		count.direction === 'future'
			? 'text-amber-700 dark:text-amber-300'
			: count.direction === 'past'
				? 'text-primary'
				: 'text-amber-600 dark:text-amber-400'
	);
</script>

<span class="inline-flex items-baseline gap-1 whitespace-nowrap {tone}">
	{#if count.direction === 'today'}
		<span class="{size === 'sm' ? 'text-lg' : 'text-2xl'} font-semibold">{$t('days.todayBig')}</span>
	{:else}
		<span class="text-[11px] text-muted-foreground">{count.direction === 'future' ? $t('days.prefixFuture') : $t('days.prefixPast')}</span>
		<span class="{size === 'sm' ? 'text-xl' : 'text-[1.75rem]'} font-semibold tabular-nums leading-none tracking-tight">{count.days.toLocaleString(intl)}</span>
		<span class="text-xs text-muted-foreground">{count.days === 1 ? $t('days.unitOne') : $t('days.unit')}</span>
	{/if}
</span>
