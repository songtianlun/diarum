<script lang="ts">
	import { onMount } from 'svelte';
	import { goto } from '$app/navigation';
	import { getToday } from '$lib/utils/date';
	import { isAuthenticated } from '$lib/api/client';
	import Footer from '$lib/components/ui/Footer.svelte';

	let ready = $state(false);

	onMount(() => {
		if ($isAuthenticated) {
			goto(`/diary/${getToday()}`);
		} else {
			ready = true;
		}
	});

	const features = [
		{
			icon: '📝',
			title: 'Daily Journaling',
			description: 'Write and organize your thoughts with a beautiful rich text editor. Support for formatting, lists, and more.'
		},
		{
			icon: '🤖',
			title: 'AI Assistant',
			description: 'Chat with an intelligent assistant that understands your diary entries and helps you reflect on your journey.'
		},
		{
			icon: '📅',
			title: 'Calendar View',
			description: 'Navigate through your entries with an intuitive calendar. See your writing streaks and activity at a glance.'
		},
		{
			icon: '🔍',
			title: 'Powerful Search',
			description: 'Find any memory instantly. Search through all your entries with full-text search capabilities.'
		},
		{
			icon: '🖼️',
			title: 'Media Library',
			description: 'Attach photos and images to your entries. Build a visual timeline of your life moments.'
		},
		{
			icon: '🌙',
			title: 'Dark Mode',
			description: 'Easy on the eyes, day or night. Seamlessly switch between light and dark themes.'
		}
	];
</script>

{#if !ready}
	<div class="flex items-center justify-center min-h-screen">
		<p class="text-muted-foreground">Loading...</p>
	</div>
{:else}
	<div class="min-h-screen flex flex-col bg-background">
		<!-- Navigation -->
		<nav class="fixed top-0 left-0 right-0 z-50 glass border-b border-border/50">
			<div class="max-w-6xl mx-auto px-4 sm:px-6 lg:px-8">
				<div class="flex items-center justify-between h-16">
					<div class="flex items-center gap-2">
						<span class="text-2xl font-bold text-foreground">Diarum</span>
					</div>
					<a
						href="/login"
						class="px-4 py-2 text-sm font-medium text-foreground hover:text-primary transition-colors"
					>
						Login
					</a>
				</div>
			</div>
		</nav>

		<!-- Hero Section -->
		<section class="pt-32 pb-20 px-4 sm:px-6 lg:px-8">
			<div class="max-w-4xl mx-auto text-center animate-fade-in">
				<h1 class="text-4xl sm:text-5xl lg:text-6xl font-bold text-foreground mb-6 leading-tight">
					Your Personal Space for
					<span class="text-primary">Daily Reflection</span>
				</h1>
				<p class="text-lg sm:text-xl text-muted-foreground mb-8 max-w-2xl mx-auto">
					Capture your thoughts, track your journey, and gain insights with AI-powered journaling.
					A beautiful, private diary that grows with you.
				</p>
				<div class="flex flex-col sm:flex-row items-center justify-center gap-4">
					<a
						href="/login"
						class="w-full sm:w-auto px-8 py-3 text-lg font-medium bg-primary text-primary-foreground rounded-xl hover:opacity-90 transition-all shadow-lg hover:shadow-xl"
					>
						Start Writing Today
					</a>
					<a
						href="#features"
						class="w-full sm:w-auto px-8 py-3 text-lg font-medium text-foreground border border-border rounded-xl hover:bg-accent transition-all"
					>
						Learn More
					</a>
				</div>
			</div>
		</section>

		<!-- Demo Screenshot Section -->
		<section class="py-16 px-4 sm:px-6 lg:px-8 bg-muted/30">
			<div class="max-w-6xl mx-auto">
				<!-- Desktop: Side by side layout -->
				<div class="hidden lg:grid lg:grid-cols-5 gap-6">
					<!-- Main Editor Panel (3 cols) -->
					<div class="col-span-3 bg-card rounded-2xl border border-border/50 shadow-2xl overflow-hidden">
						<!-- Window Chrome -->
						<div class="bg-secondary/30 px-4 py-3 border-b border-border/50 flex items-center gap-3">
							<div class="flex items-center gap-1.5">
								<div class="w-3 h-3 rounded-full bg-red-400/80"></div>
								<div class="w-3 h-3 rounded-full bg-yellow-400/80"></div>
								<div class="w-3 h-3 rounded-full bg-green-400/80"></div>
							</div>
							<span class="flex-1 text-center text-xs text-muted-foreground font-medium">Diarum</span>
						</div>
						<!-- Editor Content -->
						<div class="p-6">
							<!-- Date Header -->
							<div class="flex items-center justify-between mb-6">
								<div class="flex items-center gap-3">
									<button class="p-1.5 rounded-lg hover:bg-muted text-muted-foreground" aria-label="Previous day">
										<svg class="w-5 h-5" fill="none" stroke="currentColor" viewBox="0 0 24 24">
											<path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M15 19l-7-7 7-7"/>
										</svg>
									</button>
									<div>
										<h2 class="text-xl font-semibold text-foreground">February 1, 2026</h2>
										<p class="text-xs text-muted-foreground">Sunday</p>
									</div>
								</div>
								<div class="flex items-center gap-2">
									<span class="text-xs text-green-600 dark:text-green-400 flex items-center gap-1">
										<span class="w-1.5 h-1.5 rounded-full bg-green-500"></span>
										Saved
									</span>
								</div>
							</div>
							<!-- Toolbar -->
							<div class="flex items-center gap-1 p-2 bg-muted/30 rounded-lg mb-4">
								<button class="p-1.5 rounded hover:bg-muted text-muted-foreground" aria-label="Bold"><span class="font-bold text-sm">B</span></button>
								<button class="p-1.5 rounded hover:bg-muted text-muted-foreground" aria-label="Italic"><span class="italic text-sm">I</span></button>
								<button class="p-1.5 rounded hover:bg-muted text-muted-foreground" aria-label="Underline"><span class="underline text-sm">U</span></button>
								<div class="w-px h-4 bg-border mx-1"></div>
								<button class="p-1.5 rounded hover:bg-muted text-muted-foreground" aria-label="List">
									<svg class="w-4 h-4" fill="none" stroke="currentColor" viewBox="0 0 24 24">
										<path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M4 6h16M4 10h16M4 14h16M4 18h16"/>
									</svg>
								</button>
								<button class="p-1.5 rounded hover:bg-muted text-muted-foreground" aria-label="Image">
									<svg class="w-4 h-4" fill="none" stroke="currentColor" viewBox="0 0 24 24">
										<path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M4 16l4.586-4.586a2 2 0 012.828 0L16 16m-2-2l1.586-1.586a2 2 0 012.828 0L20 14m-6-6h.01M6 20h12a2 2 0 002-2V6a2 2 0 00-2-2H6a2 2 0 00-2 2v12a2 2 0 002 2z"/>
									</svg>
								</button>
							</div>
							<!-- Editor Text -->
							<div class="prose prose-sm text-foreground leading-relaxed space-y-3">
								<p>Today was a <strong>productive day</strong>. I finally finished the project I've been working on for the past week. It feels great to see it come together.</p>
								<p>In the afternoon, I took a long walk in the park. The weather was perfect - sunny but not too hot. I noticed the cherry blossoms are starting to bloom. 🌸</p>
								<p>Things I'm grateful for today:</p>
								<ul class="list-disc list-inside space-y-1 text-muted-foreground">
									<li>Completing my project on time</li>
									<li>The beautiful weather</li>
									<li>A good conversation with Mom</li>
								</ul>
							</div>
						</div>
					</div>

					<!-- Side Panels (2 cols) -->
					<div class="col-span-2 space-y-6">
						<!-- Calendar Panel -->
						<div class="bg-card rounded-2xl border border-border/50 shadow-xl overflow-hidden">
							<div class="bg-secondary/30 px-4 py-2.5 border-b border-border/50">
								<span class="text-sm font-medium text-foreground">February 2026</span>
							</div>
							<div class="p-4">
								<!-- Calendar Grid -->
								<div class="grid grid-cols-7 gap-1 text-center text-xs mb-2">
									<span class="text-muted-foreground">Su</span>
									<span class="text-muted-foreground">Mo</span>
									<span class="text-muted-foreground">Tu</span>
									<span class="text-muted-foreground">We</span>
									<span class="text-muted-foreground">Th</span>
									<span class="text-muted-foreground">Fr</span>
									<span class="text-muted-foreground">Sa</span>
								</div>
								<div class="grid grid-cols-7 gap-1 text-center text-xs">
									<span class="p-1.5 bg-primary text-primary-foreground rounded-lg font-medium">1</span>
									<span class="p-1.5 text-muted-foreground">2</span>
									<span class="p-1.5 text-muted-foreground">3</span>
									<span class="p-1.5 text-muted-foreground">4</span>
									<span class="p-1.5 text-muted-foreground">5</span>
									<span class="p-1.5 text-muted-foreground">6</span>
									<span class="p-1.5 text-muted-foreground">7</span>
									{#each [8,9,10,11,12,13,14,15,16,17,18,19,20,21,22,23,24,25,26,27,28] as day}
										<span class="p-1.5 text-muted-foreground relative">
											{day}
											{#if [9,10,12,15,18,19,22,25,27].includes(day)}
												<span class="absolute bottom-0.5 left-1/2 -translate-x-1/2 w-1 h-1 rounded-full bg-primary/60"></span>
											{/if}
										</span>
									{/each}
								</div>
								<!-- Stats -->
								<div class="mt-4 pt-3 border-t border-border/50 flex justify-between text-xs">
									<div class="text-center">
										<div class="text-lg font-semibold text-foreground">12</div>
										<div class="text-muted-foreground">Entries</div>
									</div>
									<div class="text-center">
										<div class="text-lg font-semibold text-foreground">5</div>
										<div class="text-muted-foreground">Streak</div>
									</div>
									<div class="text-center">
										<div class="text-lg font-semibold text-foreground">2.4k</div>
										<div class="text-muted-foreground">Words</div>
									</div>
								</div>
							</div>
						</div>

						<!-- Quick AI Chat -->
						<div class="bg-card rounded-2xl border border-border/50 shadow-xl overflow-hidden">
							<div class="bg-secondary/30 px-4 py-2.5 border-b border-border/50 flex items-center gap-2">
								<span class="text-sm">🤖</span>
								<span class="text-sm font-medium text-foreground">AI Assistant</span>
							</div>
							<div class="p-3 space-y-2.5">
								<div class="bg-muted/50 rounded-lg p-2.5">
									<p class="text-xs text-foreground">I noticed you mentioned feeling grateful today. That's wonderful! Would you like me to help you track your gratitude patterns?</p>
								</div>
								<div class="flex gap-2">
									<input type="text" placeholder="Ask anything..." class="flex-1 text-xs px-3 py-2 bg-background border border-border rounded-lg" disabled />
									<button class="p-2 bg-primary text-primary-foreground rounded-lg" aria-label="Send message">
										<svg class="w-3.5 h-3.5" fill="none" stroke="currentColor" viewBox="0 0 24 24">
											<path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M12 19l9 2-9-18-9 18 9-2zm0 0v-8"/>
										</svg>
									</button>
								</div>
							</div>
						</div>
					</div>
				</div>

				<!-- Tablet: Stacked layout -->
				<div class="hidden md:block lg:hidden space-y-6">
					<!-- Editor Panel -->
					<div class="bg-card rounded-2xl border border-border/50 shadow-2xl overflow-hidden">
						<div class="bg-secondary/30 px-4 py-3 border-b border-border/50 flex items-center gap-3">
							<div class="flex items-center gap-1.5">
								<div class="w-3 h-3 rounded-full bg-red-400/80"></div>
								<div class="w-3 h-3 rounded-full bg-yellow-400/80"></div>
								<div class="w-3 h-3 rounded-full bg-green-400/80"></div>
							</div>
							<span class="flex-1 text-center text-xs text-muted-foreground font-medium">Diarum</span>
						</div>
						<div class="p-6">
							<div class="flex items-center justify-between mb-4">
								<div>
									<h2 class="text-lg font-semibold text-foreground">February 1, 2026</h2>
									<p class="text-xs text-muted-foreground">Sunday</p>
								</div>
								<span class="text-xs text-green-600 dark:text-green-400 flex items-center gap-1">
									<span class="w-1.5 h-1.5 rounded-full bg-green-500"></span>
									Saved
								</span>
							</div>
							<div class="prose prose-sm text-foreground leading-relaxed">
								<p>Today was a <strong>productive day</strong>. I finally finished the project I've been working on for the past week.</p>
								<p>In the afternoon, I took a long walk in the park. The cherry blossoms are starting to bloom. 🌸</p>
							</div>
						</div>
					</div>
					<!-- Calendar + AI Row -->
					<div class="grid grid-cols-2 gap-4">
						<div class="bg-card rounded-xl border border-border/50 shadow-lg p-4">
							<div class="text-sm font-medium text-foreground mb-3">February 2026</div>
							<div class="grid grid-cols-7 gap-0.5 text-center text-[10px]">
								{#each ['S','M','T','W','T','F','S'] as d}
									<span class="text-muted-foreground">{d}</span>
								{/each}
								{#each Array(28) as _, i}
									<span class="p-1 {i+1 === 1 ? 'bg-primary text-primary-foreground rounded' : 'text-muted-foreground'}">{i+1}</span>
								{/each}
							</div>
						</div>
						<div class="bg-card rounded-xl border border-border/50 shadow-lg p-4">
							<div class="flex items-center gap-2 mb-3">
								<span>🤖</span>
								<span class="text-sm font-medium text-foreground">AI Assistant</span>
							</div>
							<p class="text-xs text-muted-foreground">Ask me about your entries, mood patterns, or get writing prompts...</p>
						</div>
					</div>
				</div>

				<!-- Mobile: Single column -->
				<div class="md:hidden">
					<div class="bg-card rounded-2xl border border-border/50 shadow-2xl overflow-hidden">
						<div class="bg-secondary/30 px-3 py-2.5 border-b border-border/50 flex items-center justify-between">
							<div class="flex items-center gap-1.5">
								<div class="w-2.5 h-2.5 rounded-full bg-red-400/80"></div>
								<div class="w-2.5 h-2.5 rounded-full bg-yellow-400/80"></div>
								<div class="w-2.5 h-2.5 rounded-full bg-green-400/80"></div>
							</div>
							<span class="text-xs text-muted-foreground font-medium">Diarum</span>
							<div class="w-12"></div>
						</div>
						<div class="p-4">
							<div class="flex items-center justify-between mb-3">
								<div>
									<h2 class="text-base font-semibold text-foreground">Feb 1, 2026</h2>
									<p class="text-[10px] text-muted-foreground">Sunday</p>
								</div>
								<span class="text-[10px] text-green-600 dark:text-green-400">Saved</span>
							</div>
							<div class="text-sm text-foreground leading-relaxed space-y-2">
								<p>Today was a <strong>productive day</strong>. I finally finished the project! 🎉</p>
								<p>Took a walk in the park - the cherry blossoms are blooming. 🌸</p>
							</div>
							<!-- Bottom Nav Mock -->
							<div class="mt-4 pt-3 border-t border-border/50 flex justify-around">
								<div class="text-center">
									<div class="w-8 h-8 mx-auto rounded-lg bg-primary/10 flex items-center justify-center mb-1">
										<svg class="w-4 h-4 text-primary" fill="none" stroke="currentColor" viewBox="0 0 24 24">
											<path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M11 5H6a2 2 0 00-2 2v11a2 2 0 002 2h11a2 2 0 002-2v-5m-1.414-9.414a2 2 0 112.828 2.828L11.828 15H9v-2.828l8.586-8.586z"/>
										</svg>
									</div>
									<span class="text-[10px] text-primary font-medium">Write</span>
								</div>
								<div class="text-center">
									<div class="w-8 h-8 mx-auto rounded-lg bg-muted flex items-center justify-center mb-1">
										<svg class="w-4 h-4 text-muted-foreground" fill="none" stroke="currentColor" viewBox="0 0 24 24">
											<path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M8 7V3m8 4V3m-9 8h10M5 21h14a2 2 0 002-2V7a2 2 0 00-2-2H5a2 2 0 00-2 2v12a2 2 0 002 2z"/>
										</svg>
									</div>
									<span class="text-[10px] text-muted-foreground">Calendar</span>
								</div>
								<div class="text-center">
									<div class="w-8 h-8 mx-auto rounded-lg bg-muted flex items-center justify-center mb-1">
										<svg class="w-4 h-4 text-muted-foreground" fill="none" stroke="currentColor" viewBox="0 0 24 24">
											<path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M8 12h.01M12 12h.01M16 12h.01M21 12c0 4.418-4.03 8-9 8a9.863 9.863 0 01-4.255-.949L3 20l1.395-3.72C3.512 15.042 3 13.574 3 12c0-4.418 4.03-8 9-8s9 3.582 9 8z"/>
										</svg>
									</div>
									<span class="text-[10px] text-muted-foreground">AI</span>
								</div>
								<div class="text-center">
									<div class="w-8 h-8 mx-auto rounded-lg bg-muted flex items-center justify-center mb-1">
										<svg class="w-4 h-4 text-muted-foreground" fill="none" stroke="currentColor" viewBox="0 0 24 24">
											<path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M21 21l-6-6m2-5a7 7 0 11-14 0 7 7 0 0114 0z"/>
										</svg>
									</div>
									<span class="text-[10px] text-muted-foreground">Search</span>
								</div>
							</div>
						</div>
					</div>
				</div>

				<p class="mt-6 text-center text-sm text-muted-foreground">
					Clean, distraction-free writing experience across all your devices
				</p>
			</div>
		</section>

		<!-- Features Section -->
		<section id="features" class="py-20 px-4 sm:px-6 lg:px-8">
			<div class="max-w-6xl mx-auto">
				<div class="text-center mb-16 animate-fade-in">
					<h2 class="text-3xl sm:text-4xl font-bold text-foreground mb-4">
						Everything You Need to Journal
					</h2>
					<p class="text-lg text-muted-foreground max-w-2xl mx-auto">
						Powerful features designed to make daily journaling effortless and meaningful.
					</p>
				</div>

				<div class="grid grid-cols-1 md:grid-cols-2 lg:grid-cols-3 gap-6">
					{#each features as feature, i}
						<div
							class="p-6 bg-card rounded-xl border border-border/50 hover:border-primary/30 hover:shadow-lg transition-all duration-300"
							style="animation-delay: {i * 100}ms"
						>
							<div class="text-4xl mb-4">{feature.icon}</div>
							<h3 class="text-xl font-semibold text-foreground mb-2">{feature.title}</h3>
							<p class="text-muted-foreground">{feature.description}</p>
						</div>
					{/each}
				</div>
			</div>
		</section>

		<!-- AI Assistant Preview -->
		<section class="py-20 px-4 sm:px-6 lg:px-8 bg-muted/30">
			<div class="max-w-6xl mx-auto">
				<div class="grid lg:grid-cols-2 gap-12 items-center">
					<div class="animate-fade-in">
						<h2 class="text-3xl sm:text-4xl font-bold text-foreground mb-6">
							Your AI-Powered Reflection Partner
						</h2>
						<p class="text-lg text-muted-foreground mb-6">
							Diarum's intelligent assistant reads your diary entries and helps you discover patterns,
							gain insights, and reflect on your personal growth journey.
						</p>
						<ul class="space-y-4">
							<li class="flex items-start gap-3">
								<span class="text-primary text-xl">✓</span>
								<span class="text-foreground">Ask questions about your past entries</span>
							</li>
							<li class="flex items-start gap-3">
								<span class="text-primary text-xl">✓</span>
								<span class="text-foreground">Get personalized writing prompts</span>
							</li>
							<li class="flex items-start gap-3">
								<span class="text-primary text-xl">✓</span>
								<span class="text-foreground">Discover mood patterns and trends</span>
							</li>
							<li class="flex items-start gap-3">
								<span class="text-primary text-xl">✓</span>
								<span class="text-foreground">Private and secure conversations</span>
							</li>
						</ul>
					</div>
					<div class="relative">
						<div class="bg-card rounded-2xl border border-border/50 shadow-xl overflow-hidden">
							<!-- Mock Chat Interface -->
							<div class="bg-secondary/30 px-4 py-3 border-b border-border/50">
								<span class="font-medium text-foreground">AI Assistant</span>
							</div>
							<div class="p-4 space-y-4 min-h-[300px]">
								<div class="flex gap-3">
									<div class="w-8 h-8 rounded-full bg-primary/20 flex items-center justify-center text-sm">🤖</div>
									<div class="flex-1 bg-muted/50 rounded-lg p-3">
										<p class="text-sm text-foreground">Based on your recent entries, I noticed you've been feeling more energetic this week. Would you like to explore what might be contributing to this positive change?</p>
									</div>
								</div>
								<div class="flex gap-3 justify-end">
									<div class="bg-primary/10 rounded-lg p-3 max-w-[80%]">
										<p class="text-sm text-foreground">Yes, I'd love to understand that better!</p>
									</div>
								</div>
								<div class="flex gap-3">
									<div class="w-8 h-8 rounded-full bg-primary/20 flex items-center justify-center text-sm">🤖</div>
									<div class="flex-1 bg-muted/50 rounded-lg p-3">
										<p class="text-sm text-foreground">Looking at your entries from the past two weeks, I see you started a morning routine and have been more consistent with exercise...</p>
									</div>
								</div>
							</div>
						</div>
					</div>
				</div>
			</div>
		</section>

		<!-- CTA Section -->
		<section class="py-20 px-4 sm:px-6 lg:px-8">
			<div class="max-w-3xl mx-auto text-center">
				<h2 class="text-3xl sm:text-4xl font-bold text-foreground mb-6">
					Start Your Journaling Journey Today
				</h2>
				<p class="text-lg text-muted-foreground mb-8">
					Join thousands of people who use Diarum to capture their daily thoughts and grow through reflection.
				</p>
				<a
					href="/login"
					class="inline-block px-8 py-4 text-lg font-medium bg-primary text-primary-foreground rounded-xl hover:opacity-90 transition-all shadow-lg hover:shadow-xl"
				>
					Create Your Free Account
				</a>
				<p class="mt-4 text-sm text-muted-foreground">
					No credit card required. Your data stays private.
				</p>
			</div>
		</section>

		<!-- Footer -->
		<Footer maxWidth="6xl" tagline="Your personal diary, powered by AI" />
	</div>
{/if}
