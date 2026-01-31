import { pb } from './client';

export interface Conversation {
	id: string;
	title: string;
	created: string;
	updated: string;
}

export interface Message {
	id: string;
	role: 'user' | 'assistant';
	content: string;
	referenced_diaries?: string[];
	created: string;
}

export interface ConversationDetail {
	conversation: Conversation;
	messages: Message[];
}

/**
 * Get all conversations for the current user
 */
export async function getConversations(): Promise<Conversation[]> {
	const response = await fetch('/api/ai/conversations', {
		headers: {
			'Authorization': `Bearer ${pb.authStore.token}`
		}
	});

	if (!response.ok) {
		throw new Error('Failed to get conversations');
	}

	return await response.json();
}

/**
 * Create a new conversation
 */
export async function createConversation(title?: string): Promise<Conversation> {
	const response = await fetch('/api/ai/conversations', {
		method: 'POST',
		headers: {
			'Authorization': `Bearer ${pb.authStore.token}`,
			'Content-Type': 'application/json'
		},
		body: JSON.stringify({ title: title || '' })
	});

	if (!response.ok) {
		throw new Error('Failed to create conversation');
	}

	return await response.json();
}

/**
 * Get conversation with messages
 */
export async function getConversation(id: string): Promise<ConversationDetail> {
	const response = await fetch(`/api/ai/conversations/${id}`, {
		headers: {
			'Authorization': `Bearer ${pb.authStore.token}`
		}
	});

	if (!response.ok) {
		throw new Error('Failed to get conversation');
	}

	return await response.json();
}

/**
 * Delete a conversation
 */
export async function deleteConversation(id: string): Promise<void> {
	const response = await fetch(`/api/ai/conversations/${id}`, {
		method: 'DELETE',
		headers: {
			'Authorization': `Bearer ${pb.authStore.token}`
		}
	});

	if (!response.ok) {
		throw new Error('Failed to delete conversation');
	}
}

/**
 * Update conversation title
 */
export async function updateConversationTitle(id: string, title: string): Promise<Conversation> {
	const response = await fetch(`/api/ai/conversations/${id}`, {
		method: 'PUT',
		headers: {
			'Authorization': `Bearer ${pb.authStore.token}`,
			'Content-Type': 'application/json'
		},
		body: JSON.stringify({ title })
	});

	if (!response.ok) {
		throw new Error('Failed to update conversation');
	}

	return await response.json();
}

export interface StreamChunk {
	content?: string;
	done?: boolean;
	referenced_diaries?: string[];
	error?: string;
}

/**
 * Send a message and stream the response
 */
export async function* streamChat(
	conversationId: string,
	content: string
): AsyncGenerator<StreamChunk> {
	const response = await fetch('/api/ai/chat', {
		method: 'POST',
		headers: {
			'Authorization': `Bearer ${pb.authStore.token}`,
			'Content-Type': 'application/json'
		},
		body: JSON.stringify({
			conversation_id: conversationId,
			content
		})
	});

	if (!response.ok) {
		throw new Error('Failed to send message');
	}

	if (!response.body) {
		throw new Error('No response body');
	}

	const reader = response.body.getReader();
	const decoder = new TextDecoder();
	let buffer = '';

	while (true) {
		const { done, value } = await reader.read();
		if (done) break;

		buffer += decoder.decode(value, { stream: true });
		const lines = buffer.split('\n');
		buffer = lines.pop() || '';

		for (const line of lines) {
			if (line.startsWith('data: ')) {
				try {
					const data = JSON.parse(line.slice(6));
					yield data as StreamChunk;
				} catch {
					// Skip invalid JSON
				}
			}
		}
	}
}
