<script lang="ts">
	import { onMount } from 'svelte';

	export let className = '';
	export let onNavigate: (() => void) | undefined = undefined;

	interface TocItem {
		id: string;
		text: string;
		icon: string;
	}

	const sections: TocItem[] = [
		{ id: 'api-access', text: 'API Access', icon: 'M15 7a2 2 0 012 2m4 0a6 6 0 01-7.743 5.743L11 17H9v2H7v2H4a1 1 0 01-1-1v-2.586a1 1 0 01.293-.707l5.964-5.964A6 6 0 1121 9z' },
		{ id: 'ai-assistant', text: 'AI Assistant', icon: 'M9.75 17L9 20l-1 1h8l-1-1-.75-3M3 13h18M5 17h14a2 2 0 002-2V5a2 2 0 00-2-2H5a2 2 0 00-2 2v10a2 2 0 002 2z' },
		{ id: 'image-upload', text: 'Image Upload', icon: 'M4 16l4.586-4.586a2 2 0 012.828 0L16 16m-2-2l1.586-1.586a2 2 0 012.828 0L20 14m-6-6h.01M6 20h12a2 2 0 002-2V6a2 2 0 00-2-2H6a2 2 0 00-2 2v12a2 2 0 002 2z' },
		{ id: 'sync-cache', text: 'Sync & Cache', icon: 'M4 4v5h.582m15.356 2A8.001 8.001 0 004.582 9m0 0H9m11 11v-5h-.581m0 0a8.003 8.003 0 01-15.357-2m15.357 2H15' },
		{ id: 'data-management', text: 'Data Management', icon: 'M4 7v10c0 2.21 3.582 4 8 4s8-1.79 8-4V7M4 7c0 2.21 3.582 4 8 4s8-1.79 8-4M4 7c0-2.21 3.582-4 8-4s8 1.79 8 4' }
	];

	let activeSection = '';

	function scrollToSection(id: string, updateHash = true, triggerCallback = true) {
		const el = document.getElementById(id);
		if (el) {
			el.scrollIntoView({ behavior: 'smooth', block: 'start' });
			if (updateHash) {
				history.replaceState(null, '', `#${id}`);
			}
		}
		if (triggerCallback) {
			onNavigate?.();
		}
	}

	function updateActiveSection() {
		const scrollY = window.scrollY;
		const offset = 100;

		for (let i = sections.length - 1; i >= 0; i--) {
			const el = document.getElementById(sections[i].id);
			if (el && el.offsetTop - offset <= scrollY) {
				activeSection = sections[i].id;
				return;
			}
		}
		activeSection = sections[0]?.id || '';
	}

	onMount(() => {
		// Check URL hash on mount and scroll to section
		const hash = window.location.hash.slice(1);
		if (hash && sections.some(s => s.id === hash)) {
			setTimeout(() => scrollToSection(hash, false, false), 100);
			activeSection = hash;
		} else {
			updateActiveSection();
		}

		window.addEventListener('scroll', updateActiveSection, { passive: true });
		return () => {
			window.removeEventListener('scroll', updateActiveSection);
		};
	});
</script>

<nav class="settings-toc {className}">
	<div class="text-xs font-semibold text-muted-foreground uppercase tracking-wider mb-3 px-2">
		Settings
	</div>
	<ul class="space-y-1">
		{#each sections as section, i}
			<li style="animation-delay: {i * 50}ms" class="animate-fade-in opacity-0">
				<button
					on:click={() => scrollToSection(section.id)}
					class="w-full text-left px-2 py-1.5 text-sm rounded-md flex items-center gap-2
						   transition-all duration-200 truncate
						   {activeSection === section.id
						   	? 'text-primary bg-primary/10 font-medium'
						   	: 'text-muted-foreground hover:text-foreground hover:bg-muted/50'}"
				>
					<svg class="w-4 h-4 flex-shrink-0" fill="none" stroke="currentColor" viewBox="0 0 24 24">
						<path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d={section.icon} />
					</svg>
					<span class="truncate">{section.text}</span>
				</button>
			</li>
		{/each}
	</ul>
</nav>
