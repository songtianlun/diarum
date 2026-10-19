<script lang="ts">
	import type { DayTag } from '$lib/api/days';
	import { TAG_COLORS } from '$lib/utils/days';

	/** A tag name with its colour dot. */
	let { tag, size = 'sm' }: { tag: Pick<DayTag, 'name' | 'color'>; size?: 'xs' | 'sm' } = $props();
</script>

<span class="inline-flex items-center gap-1 max-w-full rounded-full bg-muted/70 text-muted-foreground {size === 'xs' ? 'px-1.5 py-px text-[10px]' : 'px-2 py-0.5 text-xs'}">
	<span class="w-1.5 h-1.5 rounded-full flex-shrink-0" style="background: {tag.color || TAG_COLORS[6]}"></span>
	<span class="truncate">{tag.name}</span>
</span>
