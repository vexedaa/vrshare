<script>
  import { onMount, onDestroy, createEventDispatcher } from 'svelte';
  import { StartStream, StopStream, RestartStream, GetState, GetConfig, SaveConfig, GetLogEntries, DetectSystem, SwitchMonitor, HasFFmpeg, DownloadFFmpeg } from '../../wailsjs/go/gui/App';
  import { EventsOn, ClipboardSetText } from '../../wailsjs/runtime/runtime';
  import StatsRow from './StatsRow.svelte';
  import EventLog from './EventLog.svelte';
  import PresetPicker from './PresetPicker.svelte';

  const dispatch = createEventDispatcher();

  let state = { status: 'idle' };
  let config = {};
  let logEntries = [];
  let monitors = [];
  let copied = false;
  let error = '';
  let ffmpegMissing = false;
  let downloading = false;
  let unsubState;
  let unsubLog;

  onMount(async () => {
    state = await GetState();
    config = await GetConfig();
    logEntries = (await GetLogEntries()) || [];
    ffmpegMissing = !(await HasFFmpeg());
    const sysInfo = await DetectSystem();
    monitors = sysInfo.monitors || [];
    unsubState = EventsOn('stream:state', (s) => { state = s; });
    unsubLog = EventsOn('stream:log', (entries) => { logEntries = entries || []; });
  });

  onDestroy(() => {
    unsubState?.();
    unsubLog?.();
  });

  async function start() {
    error = '';
    try {
      await StartStream();
      state = await GetState();
    } catch (err) {
      error = err.toString();
      state = await GetState();
      // Check if it's an FFmpeg issue
      ffmpegMissing = !(await HasFFmpeg());
    }
  }

  async function installFFmpeg() {
    downloading = true;
    error = '';
    try {
      await DownloadFFmpeg();
      ffmpegMissing = false;
    } catch (err) {
      error = 'FFmpeg download failed: ' + err.toString();
    }
    downloading = false;
  }

  async function stop() {
    try {
      await StopStream();
      state = await GetState();
    } catch (err) {
      console.error('Stop failed:', err);
    }
  }

  async function restart() {
    try {
      await RestartStream();
      state = await GetState();
      config = await GetConfig();
    } catch (err) {
      console.error('Restart failed:', err);
    }
  }

  async function changeVolume() {
    try {
      await SaveConfig(config);
      await RestartStream();
      config = await GetConfig();
    } catch (err) {
      console.error('Volume change failed:', err);
    }
  }

  async function switchDisplay(index) {
    try {
      await SwitchMonitor(index);
      config = await GetConfig();
    } catch (err) {
      console.error('Switch monitor failed:', err);
    }
  }

  function copyURL() {
    ClipboardSetText(state.streamURL);
    copied = true;
    setTimeout(() => copied = false, 2000);
  }

  function formatUptime(ns) {
    if (!ns) return '00:00:00';
    const totalSec = Math.floor(ns / 1e9);
    const h = Math.floor(totalSec / 3600);
    const m = Math.floor((totalSec % 3600) / 60);
    const s = totalSec % 60;
    return `${String(h).padStart(2, '0')}:${String(m).padStart(2, '0')}:${String(s).padStart(2, '0')}`;
  }

  $: streaming = state.status === 'streaming';
</script>

<div class="flex justify-between items-center px-6 py-4 border-b border-slate-800">
  <div class="flex items-center gap-3">
    <div class="w-2.5 h-2.5 rounded-full {streaming ? 'bg-green-500 shadow-[0_0_6px_theme(colors.green.500)]' : 'bg-slate-500'}"></div>
    <span class="font-semibold">{streaming ? 'Streaming' : 'Idle'}</span>
    {#if streaming}
      <span class="text-slate-500">|</span>
      <span class="text-slate-400">{formatUptime(state.uptime)}</span>
    {/if}
  </div>
  <div class="flex items-center gap-4">
    {#if streaming}
      <div class="bg-slate-800 rounded-md px-3 py-1.5 flex items-center gap-2">
        <code class="text-sky-400 text-sm">{state.streamURL}</code>
        <button on:click={copyURL} class="text-slate-500 hover:text-slate-300 text-xs">
          {copied ? 'Copied!' : '[copy]'}
        </button>
      </div>
      <button on:click={restart} class="bg-slate-600 hover:bg-slate-500 text-white font-semibold px-4 py-1.5 rounded-md transition-colors">
        Restart
      </button>
      <button on:click={stop} class="bg-red-600 hover:bg-red-500 text-white font-semibold px-4 py-1.5 rounded-md transition-colors">
        Stop
      </button>
    {:else}
      <PresetPicker disabled={streaming} />
      <button on:click={start} class="bg-green-600 hover:bg-green-500 text-slate-900 font-semibold px-4 py-1.5 rounded-md transition-colors">
        Start Stream
      </button>
    {/if}
  </div>
</div>

{#if streaming}
  <StatsRow {state} />
  <div class="grid grid-cols-[250px_1fr] min-h-[250px]">
    <div class="p-4 border-r border-slate-800">
      <div class="text-xs uppercase tracking-wide text-slate-500 mb-3">Active Config</div>
      <div class="bg-slate-800 rounded-md p-3">
        <div class="font-semibold">Current</div>
        <div class="text-sm text-slate-400 mt-1">{config.resolution || 'Native'} @ {config.fps}fps</div>
        <div class="text-sm text-slate-400">{config.bitrate}kbps - Port {config.port}</div>
        <div class="text-sm text-slate-400">Audio: {config.audio ? 'On' : 'Off'}</div>
      </div>
      <button on:click={() => dispatch('openSettings')} class="text-sky-400 hover:text-sky-300 text-sm mt-4">
        Settings
      </button>
      {#if monitors.length > 1}
        <div class="mt-4">
          <div class="text-xs uppercase tracking-wide text-slate-500 mb-2">Display</div>
          <div class="flex gap-1">
            {#each monitors as mon}
              <button
                on:click={() => switchDisplay(mon.index)}
                class="w-9 h-7 rounded text-xs font-bold transition-colors flex items-center justify-center {config.monitor === mon.index ? 'bg-sky-600 text-white' : 'bg-slate-700 text-slate-400 hover:bg-slate-600'}"
                title="{mon.name} ({mon.resolution})"
              >
                {mon.index + 1}
              </button>
            {/each}
          </div>
        </div>
      {/if}
      {#if config.audio}
        <div class="mt-4">
          <div class="text-xs uppercase tracking-wide text-slate-500 mb-2">Volume ({config.audioGain ?? 6} dB)</div>
          <input type="range" bind:value={config.audioGain} min="-20" max="30" step="1"
            on:change={changeVolume}
            class="w-full accent-sky-500" />
        </div>
      {/if}
    </div>
    <EventLog entries={logEntries} />
  </div>
{:else}
  <div class="flex items-center justify-center min-h-[350px] text-slate-600">
    <div class="text-center">
      {#if error}
        <div class="bg-red-900/50 border border-red-700 rounded-md p-4 mb-4 text-red-300 text-sm text-left max-w-md mx-auto">
          <div class="font-semibold mb-1">Failed to start stream</div>
          {error}
        </div>
      {/if}
      {#if ffmpegMissing}
        <div class="bg-yellow-900/30 border border-yellow-700 rounded-md p-4 mb-4 max-w-md mx-auto">
          <div class="font-semibold text-yellow-300 mb-2">FFmpeg not found</div>
          <div class="text-sm text-yellow-200/70 mb-3">FFmpeg is required for screen capture and encoding.</div>
          <button on:click={installFFmpeg} disabled={downloading}
            class="bg-yellow-600 hover:bg-yellow-500 disabled:bg-yellow-800 text-white font-semibold px-4 py-1.5 rounded-md text-sm transition-colors">
            {downloading ? 'Downloading...' : 'Download FFmpeg'}
          </button>
        </div>
      {/if}
      <div class="text-3xl mb-2">Ready to stream</div>
      <div class="text-sm">Select a preset and click Start Stream</div>
      <div class="flex gap-4 justify-center mt-4">
        <button on:click={() => dispatch('openSettings')} class="text-sky-400 hover:text-sky-300 text-sm">
          Settings
        </button>
        <button on:click={() => dispatch('openPastLogs')} class="text-sky-400 hover:text-sky-300 text-sm">
          Past Logs
        </button>
      </div>
    </div>
  </div>
{/if}
