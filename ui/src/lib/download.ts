export function download_blob(blob: Blob, name: string) {
	const a = document.createElement('a');
	a.href = URL.createObjectURL(blob);
	a.download = name;
	a.click();
	setTimeout(() => URL.revokeObjectURL(a.href), 2000);
}

export function stamp(): string {
	return new Date().toISOString().replace(/[:T]/g, '-').slice(0, 19);
}
