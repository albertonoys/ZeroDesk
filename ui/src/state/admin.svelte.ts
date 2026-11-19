import { ApiError } from '🍎/lib/api.ts';

// Administrator actions ask for the device user's password, like the
// CardputerZero Store and macOS. The password is sent once with the action
// and never stored (agent/privileged.go).
export type AdminPrompt = {
	/** the dialog title, e.g. "ZeroDesk is trying to install new software." */
	title?: string;
	/** the confirm button, e.g. "Install Software" */
	action?: string;
	/** on a phone the sheet shows the app it is about (iOS install sheet) */
	app?: { name: string; icon?: string };
	/** the phone sheet's title and button, when they differ from the Mac's */
	source?: string;
	short?: string;
};

export const admin = $state({
	open: false,
	wrong: false,
	prompt: {} as AdminPrompt,
	resolve: null as ((password: string | null) => void) | null,
});

function ask(wrong: boolean, prompt: AdminPrompt): Promise<string | null> {
	return new Promise((resolve) => {
		admin.wrong = wrong;
		admin.prompt = prompt;
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
export async function as_admin<T>(fn: (password?: string) => Promise<T>, prompt: AdminPrompt = {}): Promise<T> {
	let password: string | undefined;
	for (let attempt = 0; ; attempt++) {
		try {
			return await fn(password);
		} catch (e) {
			if (!(e instanceof ApiError && e.need_password) || attempt >= 3) throw e;
			const p = await ask(attempt > 0, prompt);
			if (p === null) throw new Cancelled('cancelled');
			password = p;
		}
	}
}
