// Wails runtime bindings - auto-generated during wails build

export function EventsOn(eventName, callback) {
  return window.runtime.EventsOn(eventName, callback);
}

export function EventsOff(eventName) {
  return window.runtime.EventsOff(eventName);
}

export function EventsEmit(eventName, ...args) {
  return window.runtime.EventsEmit(eventName, ...args);
}

export function WindowSetTitle(title) {
  return window.runtime.WindowSetTitle(title);
}

export function ClipboardSetText(text) {
  return window.runtime.ClipboardSetText(text);
}
