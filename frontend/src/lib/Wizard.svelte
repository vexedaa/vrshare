<script>
  import { onMount, createEventDispatcher } from 'svelte';
  import { DetectSystem, SaveConfig, SaveSettings, SavePreset } from '../../wailsjs/go/gui/App';

  const dispatch = createEventDispatcher();

  let systemInfo = null;
  let loading = true;

  let encoder = 'auto';
  let monitor = 0;
  let audioEnabled = true;
  let audioDevice = '';
  let resolution = '1920x1080';
  let fps = 60;
  let bitrate = 4000;
  let port = 8080;

  onMount(async () => {
    try {
      systemInfo = await DetectSystem();
      const bestEncoder = systemInfo.encoders?.find(e => e.available);
      if (bestEncoder) encoder = bestEncoder.type;
      const primaryMonitor = systemInfo.monitors?.find(m => m.isPrimary);
      if (primaryMonitor) monitor = primaryMonitor.index;
      const defaultAudio = systemInfo.audioDevices?.find(d => d.isDefault);
      if (defaultAudio) audioDevice = defaultAudio.name;
    } catch (err) {
      console.error('Detection failed:', err);
    }
    loading = false;
  });

  async function save() {
    const cfg = { port, monitor, fps, resolution, bitrate, encoder, audio: audioEnabled, audioDevice, tunnel: '' };
    try {
      await SaveConfig(cfg);
      await SavePreset('Default', cfg);
      await SaveSettings({ firstRunComplete: true, closeBehavior: 'tray' });
      dispatch('complete');
    } catch (err) {
      console.error('Save failed:', err);
    }
  }

  const resolutionOptions = ['1920x1080', '2560x1440', '1280x720'];
  const fpsOptions = [30, 60, 120];
</script>

{#if loading}
  <div class="flex items-center justify-center min-h-screen">
    <p class="text-slate-400">Detecting system...</p>
  </div>
{:else}
  <div class="max-w-2xl mx-auto p-8">
    <div class="text-center mb-8">
      <h1 class="text-2xl font-semibold">Welcome to VRShare</h1>
      <p class="text-slate-400 mt-1">We detected your system configuration. Confirm or adjust below.</p>
    </div>

    <div class="grid grid-cols-2 gap-4">
      <div class="bg-slate-800 rounded-lg p-4">
        <div class="text-xs uppercase tracking-wide text-slate-500">Encoder</div>
        <select bind:value={encoder} class="mt-2 w-full bg-slate-900 text-slate-200 border border-slate-700 rounded px-2 py-1">
          <option value="auto">Auto (best available)</option>
          {#each (systemInfo?.encoders || []) as enc}
            <option value={enc.type} disabled={!enc.available}>
              {enc.label} {enc.available ? '' : '(unavailable)'}
            </option>
          {/each}
        </select>
      </div>

      <div class="bg-slate-800 rounded-lg p-4">
        <div class="text-xs uppercase tracking-wide text-slate-500">Monitor</div>
        <select bind:value={monitor} class="mt-2 w-full bg-slate-900 text-slate-200 border border-slate-700 rounded px-2 py-1">
          {#each (systemInfo?.monitors || []) as mon}
            <option value={mon.index}>
              {mon.name} - {mon.resolution} {mon.isPrimary ? '(Primary)' : ''}
            </option>
          {/each}
        </select>
      </div>

      <div class="bg-slate-800 rounded-lg p-4">
        <div class="text-xs uppercase tracking-wide text-slate-500">Audio</div>
        <div class="flex items-center justify-between mt-2">
          <span class="text-sm text-slate-300">System Audio</span>
          <label class="relative inline-flex items-center cursor-pointer">
            <input type="checkbox" bind:checked={audioEnabled} class="sr-only peer" />
            <div class="w-10 h-5 bg-slate-600 peer-checked:bg-green-500 rounded-full after:content-[''] after:absolute after:top-0.5 after:left-0.5 after:bg-white after:rounded-full after:h-4 after:w-4 after:transition-all peer-checked:after:translate-x-5"></div>
          </label>
        </div>
        {#if audioEnabled}
          <select bind:value={audioDevice} class="mt-2 w-full bg-slate-900 text-slate-200 border border-slate-700 rounded px-2 py-1">
            {#each (systemInfo?.audioDevices || []) as dev}
              <option value={dev.name}>{dev.name}</option>
            {/each}
          </select>
        {/if}
      </div>

      <div class="bg-slate-800 rounded-lg p-4">
        <div class="text-xs uppercase tracking-wide text-slate-500">Stream Output</div>
        <div class="grid grid-cols-2 gap-2 mt-2">
          <div>
            <div class="text-xs text-slate-400 mb-1">Resolution</div>
            <select bind:value={resolution} class="w-full bg-slate-900 text-slate-200 border border-slate-700 rounded px-2 py-1">
              {#each resolutionOptions as res}
                <option value={res}>{res}</option>
              {/each}
            </select>
          </div>
          <div>
            <div class="text-xs text-slate-400 mb-1">FPS</div>
            <select bind:value={fps} class="w-full bg-slate-900 text-slate-200 border border-slate-700 rounded px-2 py-1">
              {#each fpsOptions as f}
                <option value={f}>{f}</option>
              {/each}
            </select>
          </div>
          <div>
            <div class="text-xs text-slate-400 mb-1">Bitrate (kbps)</div>
            <input type="number" bind:value={bitrate} class="w-full bg-slate-900 text-slate-200 border border-slate-700 rounded px-2 py-1" />
          </div>
          <div>
            <div class="text-xs text-slate-400 mb-1">Port</div>
            <input type="number" bind:value={port} class="w-full bg-slate-900 text-slate-200 border border-slate-700 rounded px-2 py-1" />
          </div>
        </div>
      </div>
    </div>

    <div class="text-center mt-8">
      <button on:click={save} class="bg-blue-600 hover:bg-blue-500 text-white font-semibold px-8 py-2 rounded-lg transition-colors">
        Save & Continue
      </button>
    </div>
  </div>
{/if}
