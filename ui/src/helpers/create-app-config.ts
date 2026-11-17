import type { MsgKey } from '🍎/state/i18n.svelte.ts';

export type AppConfig = {
	/** i18n key of the window / dock title */
	title: MsgKey;

	resizable?: boolean;
	expandable?: boolean;
	/** px */
	height?: number;
	width?: number;
	min_width?: number;
	min_height?: number;

	should_open_window?: boolean;

	/** Break before this app in the dock */
	dock_breaks_before?: boolean;
	/** Native rewrite of this device app (desktop-file id); uses its icon and replaces its virtual screen */
	native_for?: string;
	/** Only in the Dock while running, like a macOS app that is not kept in the Dock */
	dock_when_running?: boolean;
	/** Hide the window chrome behind the content (content draws its own toolbar) */
	transparent_titlebar?: boolean;
};

export const create_app_config = (et: AppConfig) =>
	({
		should_open_window: true,
		dock_breaks_before: false,
		resizable: true,
		expandable: true,
		width: 720,
		height: 480,
		min_width: 360,
		min_height: 240,
		transparent_titlebar: true,
		...et,
	}) satisfies AppConfig;
