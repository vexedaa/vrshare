<script>
  import { onMount, createEventDispatcher } from 'svelte';
  import { GetConfig, SaveConfig, GetSettings, SaveSettings, DetectSystem,
           ListPresets, SavePreset, DeletePreset, GetState, RestartStream,
           GetTunnelProviders, AuthorizeTunnel } from '../../wailsjs/go/gui/App';

  const dispatch = createEventDispatcher();

  let config = {};
  let settings = {};
  let systemInfo = null;
  let presets = [];
  let tunnelProviders = [];
  let newPresetName = '';
  let error = '';
  let saved = false;
  let authMessage = '';

  onMount(async () => {
    config = await GetConfig();
    settings = await GetSettings();
    systemInfo = await DetectSystem();
    presets = (await ListPresets()) || [];
    tunnelProviders = (await GetTunnelProviders()) || [];
  });

  async function save() {
    error = '';
    try {
      await SaveConfig(config);
      await SaveSettings(settings);
      // Restart stream if currently running to apply new settings
      const state = await GetState();
      if (state.status === 'streaming') {
        await RestartStream();
      }
      saved = true;
      setTimeout(() => saved = false, 2000);
    } catch (err) {
      error = err.toString();
    }
  }

  function cancel() {
    dispatch('close');
  }

  async function saveNewPreset() {
    if (!newPresetName.trim()) return;
    try {
      await SavePreset(newPresetName.trim(), config);
      presets = (await ListPresets()) || [];
      newPresetName = '';
    } catch (err) {
      error = err.toString();
    }
  }

  async function authorize(provider) {
    authMessage = '';
    try {
      const msg = await AuthorizeTunnel(provider);
      authMessage = msg;
      tunnelProviders = (await GetTunnelProviders()) || [];
    } catch (err) {
      authMessage = err.toString();
    }
  }

  async function removePreset(name) {
    try {
      await DeletePreset(name);
      presets = (await ListPresets()) || [];
    } catch (err) {
      error = err.toString();
    }
  }

  const resolutionOptions = ['1920x1080', '2560x1440', '1280x720'];
  const fpsOptions = [30, 60, 120];
</script>

<div class="max-w-2xl mx-auto p-8">
  <div class="flex justify-between items-center mb-6">
    <h1 class="text-2xl font-semibold">Settings</h1>
    <button on:click={cancel} class="text-slate-400 hover:text-slate-200 text-sm">Back to Dashboard</button>
  </div>

  {#if error}
    <div class="bg-red-900/50 border border-red-700 rounded-md p-3 mb-4 text-red-300 text-sm">{error}</div>
  {/if}

  <section class="mb-6">
    <h2 class="text-lg font-semibold mb-3 text-slate-300">Video</h2>
    <div class="grid grid-cols-2 gap-4">
      <div>
        <label class="text-xs text-slate-400 block mb-1">Encoder</label>
        <select bind:value={config.encoder} class="w-full bg-slate-800 text-slate-200 border border-slate-700 rounded px-2 py-1.5">
          <option value="auto">Auto (best available)</option>
          {#each (systemInfo?.encoders || []) as enc}
            <option value={enc.type} disabled={!enc.available}>{enc.label}</option>
          {/each}
        </select>
      </div>
      <div>
        <label class="text-xs text-slate-400 block mb-1">Monitor</label>
        <select bind:value={config.monitor} class="w-full bg-slate-800 text-slate-200 border border-slate-700 rounded px-2 py-1.5">
          {#each (systemInfo?.monitors || []) as mon}
            <option value={mon.index}>{mon.name} - {mon.resolution}</option>
          {/each}
        </select>
      </div>
      <div>
        <label class="text-xs text-slate-400 block mb-1">Resolution</label>
        <select bind:value={config.resolution} class="w-full bg-slate-800 text-slate-200 border border-slate-700 rounded px-2 py-1.5">
          <option value="">Native</option>
          {#each resolutionOptions as res}
            <option value={res}>{res}</option>
          {/each}
        </select>
      </div>
      <div>
        <label class="text-xs text-slate-400 block mb-1">FPS</label>
        <select bind:value={config.fps} class="w-full bg-slate-800 text-slate-200 border border-slate-700 rounded px-2 py-1.5">
          {#each fpsOptions as f}
            <option value={f}>{f}</option>
          {/each}
        </select>
      </div>
      <div>
        <label class="text-xs text-slate-400 block mb-1">Bitrate (kbps)</label>
        <input type="number" bind:value={config.bitrate} min="100" max="50000"
          class="w-full bg-slate-800 text-slate-200 border border-slate-700 rounded px-2 py-1.5" />
      </div>
    </div>
  </section>

  <section class="mb-6">
    <h2 class="text-lg font-semibold mb-3 text-slate-300">Audio</h2>
    <div class="flex items-center gap-4 mb-3">
      <label class="text-sm text-slate-300">Enable audio capture</label>
      <input type="checkbox" bind:checked={config.audio} class="rounded" />
    </div>
    {#if config.audio}
      <div>
        <label class="text-xs text-slate-400 block mb-1">Audio Device</label>
        <select bind:value={config.audioDevice} class="w-full bg-slate-800 text-slate-200 border border-slate-700 rounded px-2 py-1.5">
          {#each (systemInfo?.audioDevices || []) as dev}
            <option value={dev.name}>{dev.name}</option>
          {/each}
        </select>
      </div>
    {/if}
  </section>

  <section class="mb-6">
    <h2 class="text-lg font-semibold mb-3 text-slate-300">Network</h2>
    <div class="grid grid-cols-2 gap-4">
      <div>
        <label class="text-xs text-slate-400 block mb-1">Port</label>
        <input type="number" bind:value={config.port} min="1" max="65535"
          class="w-full bg-slate-800 text-slate-200 border border-slate-700 rounded px-2 py-1.5" />
      </div>
      <div>
        <label class="text-xs text-slate-400 block mb-1">Tunnel Provider</label>
        <select bind:value={config.tunnel} class="w-full bg-slate-800 text-slate-200 border border-slate-700 rounded px-2 py-1.5">
          <option value="">None</option>
          <option value="cloudflare">Cloudflare</option>
          <option value="tailscale">Tailscale</option>
        </select>
      </div>
    </div>
  </section>

  <section class="mb-6">
    <h2 class="text-lg font-semibold mb-3 text-slate-300">Tunnel Providers</h2>
    {#if authMessage}
      <div class="bg-slate-800 border border-slate-700 rounded-md p-3 mb-3 text-slate-300 text-sm">{authMessage}</div>
    {/if}
    <div class="space-y-2">
      {#each tunnelProviders as provider}
        <div class="flex items-center justify-between bg-slate-800 rounded-md px-4 py-3">
          <div>
            <div class="font-medium text-slate-200">{provider.label}</div>
            <div class="text-xs mt-0.5 {provider.authorized ? 'text-green-400' : provider.installed ? 'text-yellow-400' : 'text-slate-500'}">
              {provider.statusText}
            </div>
          </div>
          <div>
            {#if !provider.installed}
              <span class="text-xs text-slate-500">Not installed</span>
            {:else if !provider.authorized}
              <button on:click={() => authorize(provider.name)}
                class="bg-blue-600 hover:bg-blue-500 text-white text-xs px-3 py-1 rounded transition-colors">
                Sign In
              </button>
            {:else}
              <span class="text-xs text-green-400">Ready</span>
            {/if}
          </div>
        </div>
      {/each}
    </div>
  </section>

  <section class="mb-6">
    <h2 class="text-lg font-semibold mb-3 text-slate-300">App</h2>
    <div>
      <label class="text-xs text-slate-400 block mb-1">When window is closed</label>
      <select bind:value={settings.closeBehavior} class="w-full bg-slate-800 text-slate-200 border border-slate-700 rounded px-2 py-1.5 max-w-xs">
        <option value="tray">Minimize to system tray</option>
        <option value="quit">Quit application</option>
      </select>
    </div>
  </section>

  <section class="mb-6">
    <h2 class="text-lg font-semibold mb-3 text-slate-300">Presets</h2>
    <div class="flex gap-2 mb-3">
      <input type="text" bind:value={newPresetName} placeholder="New preset name..."
        class="flex-1 bg-slate-800 text-slate-200 border border-slate-700 rounded px-2 py-1.5" />
      <button on:click={saveNewPreset} class="bg-blue-600 hover:bg-blue-500 text-white px-4 py-1.5 rounded text-sm">
        Save Current
      </button>
    </div>
    {#each presets as preset}
      <div class="flex justify-between items-center bg-slate-800 rounded px-3 py-2 mb-1">
        <span class="text-sm">{preset.name}</span>
        <button on:click={() => removePreset(preset.name)} class="text-red-400 hover:text-red-300 text-xs">Delete</button>
      </div>
    {/each}
  </section>

  <div class="flex gap-3 pt-4 border-t border-slate-800">
    <button on:click={save} class="bg-blue-600 hover:bg-blue-500 text-white font-semibold px-6 py-2 rounded-md transition-colors">
      {saved ? 'Saved!' : 'Save'}
    </button>
    <button on:click={cancel} class="bg-slate-700 hover:bg-slate-600 text-slate-200 px-6 py-2 rounded-md transition-colors">
      Cancel
    </button>
  </div>
</div>
