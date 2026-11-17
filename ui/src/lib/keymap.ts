// Browser KeyboardEvent.code -> Linux input-event-codes KEY_* value.
// The CardputerZero keyboard (tca8418) reports standard Linux key codes,
// so injecting these makes every app see a real key press.
export const KEYCODES: Record<string, number> = {
  Escape: 1, Digit1: 2, Digit2: 3, Digit3: 4, Digit4: 5, Digit5: 6, Digit6: 7,
  Digit7: 8, Digit8: 9, Digit9: 10, Digit0: 11, Minus: 12, Equal: 13,
  Backspace: 14, Tab: 15, KeyQ: 16, KeyW: 17, KeyE: 18, KeyR: 19, KeyT: 20,
  KeyY: 21, KeyU: 22, KeyI: 23, KeyO: 24, KeyP: 25, BracketLeft: 26,
  BracketRight: 27, Enter: 28, ControlLeft: 29, KeyA: 30, KeyS: 31, KeyD: 32,
  KeyF: 33, KeyG: 34, KeyH: 35, KeyJ: 36, KeyK: 37, KeyL: 38, Semicolon: 39,
  Quote: 40, Backquote: 41, ShiftLeft: 42, Backslash: 43, KeyZ: 44, KeyX: 45,
  KeyC: 46, KeyV: 47, KeyB: 48, KeyN: 49, KeyM: 50, Comma: 51, Period: 52,
  Slash: 53, ShiftRight: 54, AltLeft: 56, Space: 57, CapsLock: 58,
  ControlRight: 97, AltRight: 100, Home: 102, ArrowUp: 103, PageUp: 104,
  ArrowLeft: 105, ArrowRight: 106, End: 107, ArrowDown: 108, PageDown: 109,
  Insert: 110, Delete: 111, MetaLeft: 125, MetaRight: 126,
  F1: 59, F2: 60, F3: 61, F4: 62, F5: 63, F6: 64, F7: 65, F8: 66, F9: 67, F10: 68,
};

// On-screen CardputerZero keyboard, row by row. `c` is the Linux code,
// `w` the relative width. Labels follow the printed keycaps.
export const DEVICE_ROWS: [string, number, number?][][] = [
  [['`', 41], ['1', 2], ['2', 3], ['3', 4], ['4', 5], ['5', 6], ['6', 7], ['7', 8], ['8', 9], ['9', 10], ['0', 11], ['-', 12], ['=', 13], ['del', 14, 1.4]],
  [['tab', 15, 1.4], ['q', 16], ['w', 17], ['e', 18], ['r', 19], ['t', 20], ['y', 21], ['u', 22], ['i', 23], ['o', 24], ['p', 25], ['[', 26], [']', 27], ['\\', 43]],
  [['shift', 42, 1.4], ['caps', 58, 1.1], ['a', 30], ['s', 31], ['d', 32], ['f', 33], ['g', 34], ['h', 35], ['j', 36], ['k', 37], ['l', 38], [';', 39], ["'", 40], ['ok', 28, 1.3]],
  [['ctrl', 29, 1.2], ['opt', 56, 1.1], ['alt', 100, 1.1], ['z', 44], ['x', 45], ['c', 46], ['v', 47], ['b', 48], ['n', 49], ['m', 50], [',', 51], ['.', 52], ['/', 53], ['space', 57, 1.6]],
];

export const ARROWS: [string, number][] = [['esc', 1], ['up', 103], ['left', 105], ['down', 108], ['right', 106]];

export const KEY_BY_NAME: Record<string, number> = {
  Shift: 42, Control: 29, Alt: 56, Escape: 1, Enter: 28, Backspace: 14, Tab: 15, ' ': 57,
  ArrowUp: 103, ArrowDown: 108, ArrowLeft: 105, ArrowRight: 106, Delete: 111,
  ...Object.fromEntries('abcdefghijklmnopqrstuvwxyz'.split('').map((ch) => [ch, KEYCODES['Key' + ch.toUpperCase()]])),
  ...Object.fromEntries('0123456789'.split('').map((d) => [d, KEYCODES['Digit' + d]])),
};
