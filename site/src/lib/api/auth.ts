import { pb } from './client';

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
 * Logout current user
 */
export function logout() {
	pb.authStore.clear();
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
