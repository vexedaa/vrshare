<script>
  export let state = {};
</script>

<div class="flex items-center gap-6 px-6 py-3 bg-slate-800/50 border-b border-slate-800">
  <div class="flex items-center gap-2">
    <span class="text-xs uppercase tracking-wide text-slate-500">Viewers</span>
    <span class="text-lg font-bold text-slate-200">{state.viewerCount || 0}</span>
  </div>
</div>
