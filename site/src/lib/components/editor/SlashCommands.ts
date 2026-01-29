import { Extension } from '@tiptap/core';
import Suggestion from '@tiptap/suggestion';
import type { SuggestionOptions } from '@tiptap/suggestion';
import { getSuggestionItems, type CommandItem } from './commands';

export const SlashCommands = Extension.create({
	name: 'slashCommands',

	addOptions() {
		return {
			suggestion: {
				char: '/',
				startOfLine: false,
				command: ({ editor, range, props }: { editor: any; range: any; props: CommandItem }) => {
					props.command({ editor, range });
				},
			} as Partial<SuggestionOptions<CommandItem>>,
		};
	},

	addProseMirrorPlugins() {
		return [
			Suggestion<CommandItem>({
				editor: this.editor,
				char: this.options.suggestion.char,
				startOfLine: this.options.suggestion.startOfLine,
				items: this.options.suggestion.items,
				command: this.options.suggestion.command,
				render: this.options.suggestion.render,
			}),
		];
	},
});

export { getSuggestionItems, type CommandItem };
