import type { MenuItem } from '🍎/lib/menus.ts';

/** One floating context menu for the whole desktop. */
export const context_menu = $state({
	x: 0,
	y: 0,
	items: null as MenuItem[] | null,
});

export function show_context_menu(e: MouseEvent, items: MenuItem[]) {
	e.preventDefault();
	e.stopPropagation();
	context_menu.x = Math.min(e.clientX, innerWidth - 240);
	context_menu.y = Math.min(e.clientY, innerHeight - 30 - items.length * 26);
	context_menu.items = items;
}

export function hide_context_menu() {
	context_menu.items = null;
}
