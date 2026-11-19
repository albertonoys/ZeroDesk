// The Installer panel: one package at a time, either a .deb already on the
// Zero (Finder) or a file dropped onto ZeroDesk from the computer, which is
// copied into ~/Downloads first.
export const installer = $state({
	open: false,
	path: null as string | null,
	file: null as File | null,
});

export function install_path(path: string) {
	installer.file = null;
	installer.path = path;
	installer.open = true;
}

export function install_file(file: File) {
	installer.path = null;
	installer.file = file;
	installer.open = true;
}

export function close_installer() {
	installer.open = false;
	installer.path = null;
	installer.file = null;
}

export const is_package = (name: string) => name.toLowerCase().endsWith('.deb');
