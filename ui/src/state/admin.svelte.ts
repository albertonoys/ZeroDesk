import { ApiError } from '🍎/lib/api.ts';

// Administrator actions ask for the device user's password, like the
// CardputerZero Store and macOS. The password is sent once with the action
// and never stored (agent/privileged.go).
export const admin = $state({
	open: false,
	wrong: false,
	resolve: null as ((password: string | null) => void) | null,
});

function ask(wrong: boolean): Promise<string | null> {
	return new Promise((resolve) => {
		admin.wrong = wrong;
		admin.resolve = resolve;
		admin.open = true;
	});
}

export function answer(password: string | null) {
	const r = admin.resolve;
	admin.open = false;
	admin.resolve = null;
	r?.(password);
}

export class Cancelled extends Error {}

/** Run fn; when the device asks for the password, ask the user and try again. */
export async function as_admin<T>(fn: (password?: string) => Promise<T>): Promise<T> {
	let password: string | undefined;
	for (let attempt = 0; ; attempt++) {
		try {
			return await fn(password);
		} catch (e) {
			if (!(e instanceof ApiError && e.need_password) || attempt >= 3) throw e;
			const p = await ask(attempt > 0);
			if (p === null) throw new Cancelled('cancelled');
			password = p;
		}
	}
}
