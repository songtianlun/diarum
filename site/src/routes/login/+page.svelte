<script lang="ts">
	import { goto } from '$app/navigation';
	import { login, register, type LoginCredentials, type RegisterData } from '$lib/api/auth';
	import { onMount } from 'svelte';
	import { isAuthenticated } from '$lib/api/client';
	import Footer from '$lib/components/ui/Footer.svelte';

	let activeTab: 'login' | 'register' = 'login';
	let loading = false;
	let error = '';

	let loginForm: LoginCredentials = {
		usernameOrEmail: '',
		password: ''
	};

	let registerForm: RegisterData = {
		username: '',
		email: '',
		password: '',
		passwordConfirm: ''
	};

	onMount(() => {
		if ($isAuthenticated) {
			const today = new Date().toISOString().split('T')[0];
			goto(`/diary/${today}`);
		}
	});

	async function handleLogin() {
		loading = true;
		error = '';
		const result = await login(loginForm);
		if (result.success) {
			const today = new Date().toISOString().split('T')[0];
			goto(`/diary/${today}`);
		} else {
			error = result.error || 'Login failed';
		}
		loading = false;
	}

	async function handleRegister() {
		loading = true;
		error = '';
		if (registerForm.password !== registerForm.passwordConfirm) {
			error = 'Passwords do not match';
			loading = false;
			return;
		}
		const result = await register(registerForm);
		if (result.success) {
			const today = new Date().toISOString().split('T')[0];
			goto(`/diary/${today}`);
		} else {
			error = result.error || 'Registration failed';
		}
		loading = false;
	}
</script>

<div class="min-h-screen flex flex-col bg-background">
	<div class="flex-1 flex items-center justify-center p-4">
		<div class="w-full max-w-md animate-fade-in">
			<div class="text-center mb-8">
				<h1 class="text-3xl font-bold text-foreground mb-2">Diaria</h1>
				<p class="text-muted-foreground text-sm">Your personal diary</p>
			</div>

			<div class="bg-card rounded-xl shadow-lg border border-border/50 p-6">
				<!-- Tabs -->
				<div class="flex border-b border-border mb-6">
					<button
						class="flex-1 py-2 px-4 text-center text-sm font-medium transition-all duration-200
							   {activeTab === 'login'
							? 'text-primary border-b-2 border-primary'
							: 'text-muted-foreground hover:text-foreground'}"
						on:click={() => { activeTab = 'login'; error = ''; }}
					>
						Login
					</button>
					<button
						class="flex-1 py-2 px-4 text-center text-sm font-medium transition-all duration-200
							   {activeTab === 'register'
							? 'text-primary border-b-2 border-primary'
							: 'text-muted-foreground hover:text-foreground'}"
						on:click={() => { activeTab = 'register'; error = ''; }}
					>
						Register
					</button>
				</div>

				{#if error}
					<div class="mb-4 p-3 bg-red-500/10 border border-red-500/20 text-red-600 dark:text-red-400 rounded-lg text-sm animate-fade-in">
						{error}
					</div>
				{/if}

				{#if activeTab === 'login'}
					<form on:submit|preventDefault={handleLogin} class="space-y-4">
						<div>
							<label for="usernameOrEmail" class="block text-sm font-medium text-foreground mb-1.5">
								Username or Email
							</label>
							<input
								id="usernameOrEmail"
								type="text"
								bind:value={loginForm.usernameOrEmail}
								required
								class="w-full px-3 py-2 bg-background border border-border rounded-lg
									   focus:outline-none focus:ring-2 focus:ring-primary/50 focus:border-primary
									   text-foreground placeholder:text-muted-foreground transition-all duration-200"
								placeholder="Enter your username or email"
							/>
						</div>

						<div>
							<label for="password" class="block text-sm font-medium text-foreground mb-1.5">
								Password
							</label>
							<input
								id="password"
								type="password"
								bind:value={loginForm.password}
								required
								class="w-full px-3 py-2 bg-background border border-border rounded-lg
									   focus:outline-none focus:ring-2 focus:ring-primary/50 focus:border-primary
									   text-foreground placeholder:text-muted-foreground transition-all duration-200"
								placeholder="Enter your password"
							/>
						</div>

						<button
							type="submit"
							disabled={loading}
							class="w-full py-2.5 px-4 bg-primary text-primary-foreground rounded-lg font-medium
								   hover:opacity-90 transition-all duration-200 disabled:opacity-50"
						>
							{loading ? 'Logging in...' : 'Login'}
						</button>
					</form>
				{:else}
					<form on:submit|preventDefault={handleRegister} class="space-y-4">
						<div>
							<label for="username" class="block text-sm font-medium text-foreground mb-1.5">
								Username
							</label>
							<input
								id="username"
								type="text"
								bind:value={registerForm.username}
								required
								minlength="3"
								class="w-full px-3 py-2 bg-background border border-border rounded-lg
									   focus:outline-none focus:ring-2 focus:ring-primary/50 focus:border-primary
									   text-foreground placeholder:text-muted-foreground transition-all duration-200"
								placeholder="Choose a username"
							/>
						</div>

						<div>
							<label for="email" class="block text-sm font-medium text-foreground mb-1.5">
								Email
							</label>
							<input
								id="email"
								type="email"
								bind:value={registerForm.email}
								required
								class="w-full px-3 py-2 bg-background border border-border rounded-lg
									   focus:outline-none focus:ring-2 focus:ring-primary/50 focus:border-primary
									   text-foreground placeholder:text-muted-foreground transition-all duration-200"
								placeholder="Enter your email"
							/>
						</div>

						<div>
							<label for="registerPassword" class="block text-sm font-medium text-foreground mb-1.5">
								Password
							</label>
							<input
								id="registerPassword"
								type="password"
								bind:value={registerForm.password}
								required
								minlength="8"
								class="w-full px-3 py-2 bg-background border border-border rounded-lg
									   focus:outline-none focus:ring-2 focus:ring-primary/50 focus:border-primary
									   text-foreground placeholder:text-muted-foreground transition-all duration-200"
								placeholder="Choose a password (min 8 chars)"
							/>
						</div>

						<div>
							<label for="passwordConfirm" class="block text-sm font-medium text-foreground mb-1.5">
								Confirm Password
							</label>
							<input
								id="passwordConfirm"
								type="password"
								bind:value={registerForm.passwordConfirm}
								required
								class="w-full px-3 py-2 bg-background border border-border rounded-lg
									   focus:outline-none focus:ring-2 focus:ring-primary/50 focus:border-primary
									   text-foreground placeholder:text-muted-foreground transition-all duration-200"
								placeholder="Confirm your password"
							/>
						</div>

						<button
							type="submit"
							disabled={loading}
							class="w-full py-2.5 px-4 bg-primary text-primary-foreground rounded-lg font-medium
								   hover:opacity-90 transition-all duration-200 disabled:opacity-50"
						>
							{loading ? 'Creating account...' : 'Create Account'}
						</button>
					</form>
				{/if}
			</div>
		</div>
	</div>

	<!-- Footer -->
	<Footer maxWidth="md" tagline="A simple, private diary" />
</div>
