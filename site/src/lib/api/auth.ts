import { pb, type User } from './client';

export interface LoginCredentials {
	usernameOrEmail: string;
	password: string;
}

export interface RegisterData {
	username: string;
	email: string;
	password: string;
	passwordConfirm: string;
}

/**
 * Login with username/email and password
 */
export async function login(credentials: LoginCredentials) {
	try {
		const response = await fetch('/api/v1/auth/login', {
			method: 'POST',
			headers: {
				'Content-Type': 'application/json'
			},
			body: JSON.stringify(credentials)
		});

		const data = await response.json();
		if (!response.ok) {
			return {
				success: false,
				error: data?.message || 'Login failed'
			};
		}

		if (data?.token && data?.record) {
			pb.authStore.save(data.token, data.record);
		}

		return { success: true, data };
	} catch (error: any) {
		return {
			success: false,
			error: error.message || 'Login failed'
		};
	}
}

/**
 * Register a new user
 */
export async function register(data: RegisterData) {
	try {
		const response = await fetch('/api/v1/auth/register', {
			method: 'POST',
			headers: {
				'Content-Type': 'application/json'
			},
			body: JSON.stringify(data)
		});

		const result = await response.json();
		if (!response.ok) {
			return {
				success: false,
				error: result?.message || 'Registration failed'
			};
		}

		// Auto-login after registration
		await login({
			usernameOrEmail: data.username,
			password: data.password
		});
		return { success: true, data: result };
	} catch (error: any) {
		return {
			success: false,
			error: error.message || 'Registration failed'
		};
	}
}

/**
 * Logout current user. The server is told first so the sign-out is audited;
 * the local session is cleared regardless of whether that call succeeds.
 */
export async function logout() {
	const token = pb.authStore.token;
	if (token) {
		try {
			await fetch('/api/v1/auth/logout', {
				method: 'POST',
				headers: { Authorization: `Bearer ${token}` }
			});
		} catch {
			// Offline or server gone: signing out locally still works.
		}
	}
	pb.authStore.clear();
}

/**
 * Refresh the signed-in account (including its role) from the server and
 * keep the stored copy in sync. Returns null when signed out.
 */
export async function fetchCurrentUser(): Promise<User | null> {
	if (!pb.authStore.token) return null;
	const response = await fetch('/api/v1/auth/me', {
		headers: { Authorization: `Bearer ${pb.authStore.token}` }
	});
	if (!response.ok) return null;
	const user = (await response.json()) as User;
	if (pb.authStore.token) pb.authStore.save(pb.authStore.token, user);
	return user;
}

/**
 * Check if user is authenticated
 */
export function isLoggedIn(): boolean {
	return pb.authStore.isValid;
}

/**
 * Get current user
 */
export function getCurrentUser() {
	return pb.authStore.model;
}
