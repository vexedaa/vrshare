<script>
  export let state = {};
</script>

<div class="grid grid-cols-5 gap-px bg-slate-800">
  <div class="bg-slate-900 p-4 text-center">
    <div class="text-xs uppercase tracking-wide text-slate-500">FPS</div>
    <div class="text-2xl font-bold" class:text-green-400={state.fps >= 58} class:text-yellow-400={state.fps > 0 && state.fps < 58} class:text-slate-200={!state.fps}>
      {state.fps?.toFixed(1) || '0.0'}
    </div>
  </div>
  <div class="bg-slate-900 p-4 text-center">
    <div class="text-xs uppercase tracking-wide text-slate-500">Bitrate</div>
    <div class="text-2xl font-bold text-slate-200">
      {state.bitrate ? state.bitrate.toLocaleString() : '0'} <span class="text-sm text-slate-400">kbps</span>
    </div>
  </div>
  <div class="bg-slate-900 p-4 text-center">
    <div class="text-xs uppercase tracking-wide text-slate-500">Dropped</div>
    <div class="text-2xl font-bold" class:text-slate-200={!state.droppedFrames} class:text-red-400={state.droppedFrames > 0}>
      {state.droppedFrames || 0}
    </div>
  </div>
  <div class="bg-slate-900 p-4 text-center">
    <div class="text-xs uppercase tracking-wide text-slate-500">Speed</div>
    <div class="text-2xl font-bold" class:text-green-400={state.speed >= 0.98} class:text-yellow-400={state.speed > 0 && state.speed < 0.98}>
      {state.speed?.toFixed(2) || '0.00'}<span class="text-sm text-slate-400">x</span>
    </div>
  </div>
  <div class="bg-slate-900 p-4 text-center">
    <div class="text-xs uppercase tracking-wide text-slate-500">Viewers</div>
    <div class="text-2xl font-bold text-slate-200">{state.viewerCount || 0}</div>
  </div>
</div>
