<script>
  export let entries = [];

  function formatTime(timeStr) {
    if (!timeStr) return '';
    const d = new Date(timeStr);
    return d.toLocaleTimeString('en-US', { hour12: false });
  }
</script>

<div class="p-4">
  <div class="text-xs uppercase tracking-wide text-slate-500 mb-3">Event Log</div>
  <div class="font-mono text-sm text-slate-400 space-y-1 max-h-48 overflow-y-auto">
    {#each [...entries].reverse() as entry}
      <div>
        <span class="text-slate-600">{formatTime(entry.time)}</span>
        {entry.message}
      </div>
    {:else}
      <div class="text-slate-600">No events</div>
    {/each}
  </div>
</div>
