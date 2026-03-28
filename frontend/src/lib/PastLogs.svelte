<script>
  import { onMount, createEventDispatcher } from 'svelte';
  import { ListSessionLogs, ReadSessionLog } from '../../wailsjs/go/gui/App';

  const dispatch = createEventDispatcher();

  let logs = [];
  let selectedLog = null;
  let logContent = '';
  let loading = true;

  onMount(async () => {
    try {
      logs = (await ListSessionLogs()) || [];
    } catch (err) {
      console.error('Failed to list logs:', err);
    }
    loading = false;
  });

  async function viewLog(entry) {
    selectedLog = entry;
    try {
      logContent = await ReadSessionLog(entry.name);
    } catch (err) {
      logContent = 'Failed to read log: ' + err.toString();
    }
  }

  function back() {
    if (selectedLog) {
      selectedLog = null;
      logContent = '';
    } else {
      dispatch('close');
    }
  }

  function formatSize(bytes) {
    if (bytes < 1024) return bytes + ' B';
    return (bytes / 1024).toFixed(1) + ' KB';
  }
</script>

<div class="flex justify-between items-center px-6 py-4 border-b border-slate-800">
  <div class="flex items-center gap-3">
    <button on:click={back} class="text-slate-400 hover:text-slate-200 text-sm">&larr; Back</button>
    <span class="font-semibold">{selectedLog ? selectedLog.date : 'Past Logs'}</span>
  </div>
</div>

<div class="p-6">
  {#if loading}
    <p class="text-slate-500">Loading...</p>
  {:else if selectedLog}
    <pre class="font-mono text-sm text-slate-400 bg-slate-800/50 rounded-md p-4 max-h-[420px] overflow-y-auto whitespace-pre-wrap">{logContent || 'Empty log'}</pre>
  {:else if logs.length === 0}
    <p class="text-slate-500 text-center mt-12">No past session logs found.</p>
  {:else}
    <div class="space-y-1">
      {#each logs as entry}
        <button on:click={() => viewLog(entry)}
          class="w-full text-left px-4 py-3 rounded-md bg-slate-800/50 hover:bg-slate-700/50 transition-colors flex justify-between items-center">
          <span class="text-slate-300 text-sm">{entry.date}</span>
          <span class="text-slate-600 text-xs">{formatSize(entry.size)}</span>
        </button>
      {/each}
    </div>
  {/if}
</div>
