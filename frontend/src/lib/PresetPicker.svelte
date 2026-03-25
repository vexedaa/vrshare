<script>
  import { onMount, createEventDispatcher } from 'svelte';
  import { ListPresets, LoadPreset } from '../../wailsjs/go/gui/App';

  const dispatch = createEventDispatcher();

  export let disabled = false;
  let presets = [];
  let selectedName = 'Default';

  onMount(loadPresets);

  async function loadPresets() {
    try {
      presets = (await ListPresets()) || [];
      if (presets.length > 0 && !presets.find(p => p.name === selectedName)) {
        selectedName = presets[0].name;
      }
    } catch (err) {
      console.error('Failed to load presets:', err);
    }
  }

  async function onSelect(event) {
    selectedName = event.target.value;
    try {
      const cfg = await LoadPreset(selectedName);
      dispatch('loaded', { name: selectedName, config: cfg });
    } catch (err) {
      console.error('Failed to load preset:', err);
    }
  }
</script>

<select value={selectedName} on:change={onSelect} {disabled}
  class="bg-slate-800 text-slate-200 border border-slate-700 rounded-md px-3 py-1.5 text-sm">
  {#each presets as preset}
    <option value={preset.name}>{preset.name}</option>
  {/each}
</select>
