// @ts-check
// Wails bindings for gui.App - auto-generated during wails build

export function StartStream() {
  return window['go']['gui']['App']['StartStream']();
}

export function StopStream() {
  return window['go']['gui']['App']['StopStream']();
}

export function GetState() {
  return window['go']['gui']['App']['GetState']();
}

export function GetConfig() {
  return window['go']['gui']['App']['GetConfig']();
}

export function SaveConfig(cfg) {
  return window['go']['gui']['App']['SaveConfig'](cfg);
}

export function ListPresets() {
  return window['go']['gui']['App']['ListPresets']();
}

export function SavePreset(name, cfg) {
  return window['go']['gui']['App']['SavePreset'](name, cfg);
}

export function LoadPreset(name) {
  return window['go']['gui']['App']['LoadPreset'](name);
}

export function DeletePreset(name) {
  return window['go']['gui']['App']['DeletePreset'](name);
}

export function DetectSystem() {
  return window['go']['gui']['App']['DetectSystem']();
}

export function GetSettings() {
  return window['go']['gui']['App']['GetSettings']();
}

export function SaveSettings(s) {
  return window['go']['gui']['App']['SaveSettings'](s);
}

export function GetLogEntries() {
  return window['go']['gui']['App']['GetLogEntries']();
}

export function ShowWindow() {
  return window['go']['gui']['App']['ShowWindow']();
}
