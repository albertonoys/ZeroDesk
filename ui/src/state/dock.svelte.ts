import { persisted } from './persisted.svelte.ts';

export const is_dock_hidden = $state({ value: false as boolean });

/** The apps kept in the Dock, in the user's order (built-in app ids and dev:<id> for device apps), per browser. */
export const dock_order = persisted('zerodesk:dock-order', { ids: [] as string[] });

/** Set by the Dock: how many apps it can keep at the smallest icon size on this screen. */
export const dock_room = $state({ capacity: Infinity });
