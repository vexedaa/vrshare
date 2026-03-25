<script>
  import { onMount } from 'svelte';
  import Wizard from './lib/Wizard.svelte';
  import Dashboard from './lib/Dashboard.svelte';
  import Settings from './lib/Settings.svelte';
  import { GetSettings } from '../wailsjs/go/gui/App';

  let view = 'loading';

  onMount(async () => {
    try {
      const settings = await GetSettings();
      view = settings.firstRunComplete ? 'dashboard' : 'wizard';
    } catch {
      view = 'wizard';
    }
  });

  function onWizardComplete() {
    view = 'dashboard';
  }

  function onOpenSettings() {
    view = 'settings';
  }

  function onCloseSettings() {
    view = 'dashboard';
  }
</script>

<main class="min-h-screen bg-slate-900 text-slate-200">
  {#if view === 'loading'}
    <div class="flex items-center justify-center min-h-screen">
      <p class="text-slate-400 text-lg">Loading...</p>
    </div>
  {:else if view === 'wizard'}
    <Wizard on:complete={onWizardComplete} />
  {:else if view === 'dashboard'}
    <Dashboard on:openSettings={onOpenSettings} />
  {:else if view === 'settings'}
    <Settings on:close={onCloseSettings} />
  {/if}
</main>
