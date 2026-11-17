/** Ask an open Finder window to navigate somewhere (e.g. the Dock's Trash). */
export const finder_nav = $state({ target: '', seq: 0 });

export const APPS_LOCATION = 'zd:applications';
export const TRASH_LOCATION = 'zd:trash';

export function finder_go(target: string) {
	finder_nav.target = target;
	finder_nav.seq++;
}

export const FILE_MIME = 'application/x-zerodesk-file';
