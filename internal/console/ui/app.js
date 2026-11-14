'use strict';
const $ = id => document.getElementById(id);
const node = (tag, text, cls) => {
  const n = document.createElement(tag);
  if (text !== undefined) n.textContent = text;
  if (cls) n.className = cls;
  return n;
};
const cpu = n => `${((n || 0) / 1000).toLocaleString(undefined, {maximumFractionDigits: 2})} cores`;
const memory = n => `${((n || 0) / 1073741824).toLocaleString(undefined, {maximumFractionDigits: 2})} GiB`;
const date = value => value ? new Date(value).toLocaleString() : 'Not received';
const state = {view: 'workloads', cluster: '', clusters: [], items: [], cursor: '', request: 0, detail: 0};
function stale(c) {return !c.receivedAt || !c.capturedAt || Date.now() - new Date(c.receivedAt).getTime() > 180000 || Date.now() - new Date(c.capturedAt).getTime() > 180000;}
function label(id) {return state.clusters.find(c => c.id === id)?.name || id;}
async function request(path, options = {}) {
  const response = await fetch(path, {cache: 'no-store', credentials: 'same-origin', ...options});
  if (response.status === 401) {showLogin(); throw new Error('Sign in to continue.');}
  if (!response.ok) throw new Error((await response.text()).trim() || `Request failed (${response.status})`);
  return response.status === 204 ? null : response.json();
}
function showLogin() {$('console').hidden = true; $('login').hidden = false; if ($('detail').open) $('detail').close();}
async function boot() {
  try {
    const auth = await request('/auth/config'); $('sso').hidden = !auth.oidc;
    const me = await request('/api/me'); $('identity').textContent = me.subject;
    $('login').hidden = true; $('console').hidden = false;
    await load();
  } catch (error) {if (!$('console').hidden) status(error.message, true); else {showLogin(); $('loginerror').textContent = error.message;}}
}
function status(message, error = false) {$('status').textContent = message; $('status').className = error ? 'error' : '';}
function filters() {
  const p = new URLSearchParams({limit: '50'});
  for (const [key, value] of [['cluster', state.cluster], ['namespace', $('namespace').value.trim()], ['search', $('search').value.trim()], ['mode', $('mode').value]]) if (value) p.set(key, value);
  if ($('deleted').checked) p.set('deleted', 'true');
  return p;
}
async function load(more = false) {
  const version = ++state.request;
  $('refresh').disabled = true; $('more').disabled = true;
  status('Loading observations…');
  try {
    const p = filters(); if (more && state.cursor) p.set('cursor', state.cursor);
    const [clusters, summary, page] = await Promise.all([
      request('/api/clusters'), request('/api/summary?' + new URLSearchParams({cluster: state.cluster})), request(`/api/${state.view}?${p}`)
    ]);
    if (version !== state.request) return;
    state.clusters = clusters; state.items = more ? state.items.concat(page.items) : page.items; state.cursor = page.nextCursor || '';
    renderClusters(); renderSummary(summary); renderRows();
    const scoped = clusters.filter(c => !state.cluster || c.id === state.cluster);
    const delayed = scoped.filter(stale);
    const warnings = scoped.flatMap(c => (c.warnings || []).map(w => `${c.name}: ${w}`));
    status(delayed.length ? `${delayed.length} cluster(s) have delayed observations. Showing the last persisted state.` : `Synced view · ${new Date().toLocaleTimeString()}`);
    if (warnings.length) status(warnings.join(' · '), true);
  } catch (error) {if (version === state.request) status('Could not refresh: ' + error.message, true);}
  finally {if (version === state.request) {$('refresh').disabled = false; $('more').disabled = false;}}
}
function renderClusters() {
  $('clusterlist').replaceChildren();
  for (const c of state.clusters) {
    const b = node('button', undefined, 'cluster' + (c.id === state.cluster ? ' selected' : ''));
    b.append(node('span', undefined, 'dot' + (stale(c) ? ' stale' : '')), node('span', c.name), node('small', 'Observed ' + date(c.capturedAt)));
    b.onclick = () => {state.cluster = c.id; load();}; $('clusterlist').append(b);
  }
  if (!state.clusters.length) $('clusterlist').append(node('small', 'Waiting for the first agent upload.'));
  $('scope').textContent = state.cluster ? label(state.cluster).toUpperCase() : 'ALL CLUSTERS';
}
function renderSummary(s) {
  $('summary').replaceChildren();
  const cards = [['Selected workloads', s.workloads, 'Within your access scope'], ['CPU requested', cpu(s.requestedMilliCPU), 'Across observed active pods'], ['Memory requested', memory(s.requestedMemoryBytes), 'Across observed active pods'], ['History observations', s.records, `${s.policies} active policies`]];
  for (const [title, value, detail] of cards) {const c = node('div', undefined, 'stat'); c.append(node('span', title), node('strong', value), node('small', detail)); $('summary').append(c);}
}
function badge(value) {return node('span', value || 'Unknown', 'badge ' + (value || ''));}
function cell(title, subtitle) {const c = node('td'); c.append(node('strong', title)); if (subtitle) c.append(node('small', subtitle)); return c;}
function renderRows() {
  const names = {workloads: 'Fleet overview', policies: 'Policies', history: 'Decision history'};
  $('title').textContent = names[state.view];
  $('subtitle').textContent = {workloads: 'Understand what is running, what changed, and why.', policies: 'Review observed policy settings and their revisions.', history: 'Trace recommendations, skipped decisions and applied changes.'}[state.view];
  $('mode').hidden = state.view !== 'workloads'; $('deleted').parentElement.hidden = state.view === 'history';
  $('thead').replaceChildren(); $('rows').replaceChildren();
  const columns = {workloads: ['Workload', 'Cluster', 'Mode', 'Ready pods', 'CPU request', 'Memory request', ''], policies: ['Policy', 'Cluster', 'Mode', 'Revision', 'Observed', ''], history: ['Workload', 'Cluster', 'Outcome', 'Method', 'Recorded', '']}[state.view];
  const head = node('tr'); columns.forEach(c => head.append(node('th', c))); $('thead').append(head);
  for (const item of state.items) {
    const d = item.data; const row = node('tr');
    if (state.view === 'workloads') {
      row.append(cell(d.name, `${d.namespace} · ${d.kind}${item.deleted ? ' · Removed' : ''}`), cell(label(item.clusterID)));
      const mode = node('td'); mode.append(badge(d.paused ? 'Paused' : d.mode)); row.append(mode);
      row.append(cell(`${(d.pods || []).filter(p => p.ready).length} / ${(d.pods || []).length}`), cell(cpu(d.requestedMilliCPU)), cell(memory(d.requestedMemoryBytes)));
    } else if (state.view === 'policies') {
      row.append(cell(d.name, `${d.namespace}${item.deleted ? ' · Removed' : ''}`), cell(label(item.clusterID)));
      const mode = node('td'); mode.append(badge(d.spec.mode)); row.append(mode, cell(`v${d.generation}`), cell(date(item.observedAt)));
    } else {
      row.append(cell(d.workload.Name, `${d.workload.Namespace} · ${d.workload.Kind}`), cell(label(item.clusterID)));
      const phase = node('td'); phase.append(badge(d.phase)); row.append(phase, cell(d.method || 'Decision'), cell(date(d.created)));
    }
    const action = node('td'); const b = node('button', 'Inspect →'); b.setAttribute('aria-label', 'Inspect ' + (d.name || d.workload?.Name)); b.onclick = () => inspect(item); action.append(b); row.append(action); $('rows').append(row);
  }
  if (!state.items.length) {const row = node('tr'), c = node('td', 'No observations match these filters.', 'empty'); c.colSpan = columns.length; row.append(c); $('rows').append(row);}
  $('more').hidden = !state.cursor; $('pageinfo').textContent = `${state.items.length} observations shown${state.cursor ? ' · more available' : ''}`;
}
function jsonSection(title, value) {const d = node('details'); d.append(node('summary', title), node('pre', JSON.stringify(value, null, 2))); return d;}
function resourceTable(containers) {
  const table = node('table', undefined, 'detailtable'), head = node('tr');
  ['Container', 'Current CPU', 'Recommended CPU', 'Current memory', 'Recommended memory'].forEach(x => head.append(node('th', x))); table.append(head);
  for (const c of containers || []) {const row = node('tr'); [c.name, c.current?.requests?.cpu || 'Unset', c.recommended?.['cpu-request'] || 'Collecting', c.current?.requests?.memory || 'Unset', c.recommended?.['memory-request'] || 'Collecting'].forEach(x => row.append(node('td', x))); table.append(row);}
  return table;
}
function chart(samples) {
  const points = samples.items.slice().reverse(); if (points.length < 2) return node('p', 'At least two observations are needed for the resource-request chart.', 'notebox');
  const ns = 'http://www.w3.org/2000/svg'; const svg = document.createElementNS(ns, 'svg'); svg.setAttribute('viewBox', '0 0 720 150'); svg.setAttribute('class', 'chart'); svg.setAttribute('role', 'img'); svg.setAttribute('aria-label', 'Observed requested CPU over time');
  const max = Math.max(1, ...points.map(p => p.data.requestedMilliCPU)); const start = new Date(points[0].observedAt).getTime(), end = new Date(points.at(-1).observedAt).getTime();
  const line = document.createElementNS(ns, 'polyline'); line.setAttribute('points', points.map(p => `${30 + ((new Date(p.observedAt).getTime() - start) / Math.max(1, end - start)) * 660},${115 - (p.data.requestedMilliCPU / max) * 85}`).join(' ')); svg.append(line);
  for (const [x, y, value] of [[15, 20, cpu(max)], [15, 140, date(points[0].observedAt)], [465, 140, date(points.at(-1).observedAt)]]) {const text = document.createElementNS(ns, 'text'); text.setAttribute('x', x); text.setAttribute('y', y); text.textContent = value; svg.append(text);}
  return svg;
}
function differences(before, after, path = '') {
  const out = [];
  for (const key of new Set([...Object.keys(before || {}), ...Object.keys(after || {})])) {
    const a = before?.[key], b = after?.[key], p = path ? path + '.' + key : key;
    if (JSON.stringify(a) === JSON.stringify(b)) continue;
    if (a && b && typeof a === 'object' && typeof b === 'object' && !Array.isArray(a) && !Array.isArray(b)) out.push(...differences(a, b, p));
    else out.push(`${p}: ${JSON.stringify(a) ?? '(absent)'} → ${JSON.stringify(b) ?? '(absent)'}`);
  }
  return out;
}
async function inspect(item) {
  const version = ++state.detail, view = state.view, d = item.data, body = $('detailbody'); body.replaceChildren();
  $('detailcluster').textContent = label(item.clusterID); $('detailtitle').textContent = view === 'history' ? d.workload.Name : d.name;
  $('detail').showModal();
  const meta = node('div', undefined, 'detailmeta'); meta.append(badge(view === 'history' ? d.phase : view === 'policies' ? d.spec.mode : d.mode), node('small', 'Observed ' + date(item.observedAt))); body.append(meta);
  if (view === 'history') {body.append(node('p', d.decision?.reason || d.error || 'Observed action record.', 'notebox'), jsonSection('Complete observed record', d)); return;}
  if (view === 'workloads') {
    body.append(node('p', d.reason || 'No recent processing reason was observed.', 'notebox'), resourceTable(d.containers), jsonSection('Actual pods and resources', d.pods || []));
    if (d.nodePlans) body.append(jsonSection('Node sizing plans', d.nodePlans));
  }
  const p = new URLSearchParams({cluster: item.clusterID, namespace: d.namespace, uid: d.uid, limit: '100'});
  const loading = node('p', 'Loading persistent history…', 'notebox'); body.append(loading);
  try {
    if (view === 'workloads') {
      const [history, samples] = await Promise.all([request('/api/history?' + p), request('/api/samples?' + p)]);
      if (version !== state.detail) return; loading.remove();
      body.append(node('h3', 'Requested CPU over time'), chart(samples), node('small', 'Allocation observations, not CPU usage measurements. Most recent 100 points.'), node('h3', 'Observed decision and action history'));
      if (!history.items.length) body.append(node('p', 'No exported history is available yet.', 'notebox'));
      for (const record of history.items) {const r = record.data; body.append(jsonSection(`${date(r.created)} · ${r.phase} · ${r.decision?.reason || r.method || r.mode}`, r));}
      if (history.nextCursor) body.append(node('small', 'More history is available in the Decision history view.'));
    } else {
      const revisions = await request('/api/revisions?' + p); if (version !== state.detail) return; loading.remove();
      body.append(jsonSection('Current observed policy', d.spec));
      if (revisions.items.length > 1) body.append(jsonSection('Changes between the latest observed revisions', differences(revisions.items[1].data.spec, revisions.items[0].data.spec)));
      body.append(node('h3', 'Policy revision history'));
      for (const revision of revisions.items) body.append(jsonSection(`v${revision.data.generation} · ${date(revision.observedAt)}`, revision.data.spec));
    }
  } catch (error) {if (version === state.detail) {loading.textContent = error.message; loading.className = 'error';}}
}
$('loginform').onsubmit = async event => {event.preventDefault(); $('loginerror').textContent = ''; try {await request('/auth/login', {method: 'POST', headers: {'Content-Type': 'application/json'}, body: JSON.stringify({token: $('token').value})}); $('token').value = ''; await boot();} catch (error) {$('loginerror').textContent = error.message;}};
$('logout').onclick = async () => {try {await request('/auth/logout', {method: 'POST'}); showLogin();} catch (error) {status(error.message, true);}};
$('refresh').onclick = () => load(); $('more').onclick = () => load(true);
$('allclusters').onclick = () => {state.cluster = ''; load();};
for (const b of document.querySelectorAll('nav button')) b.onclick = () => {state.view = b.dataset.view; for (const n of document.querySelectorAll('nav button')) n.classList.toggle('active', n === b); state.cursor = ''; load();};
let debounce;
for (const id of ['search', 'namespace']) $(id).oninput = () => {clearTimeout(debounce); debounce = setTimeout(() => load(), 250);};
for (const id of ['mode', 'deleted']) $(id).onchange = () => load();
$('close').onclick = () => $('detail').close(); $('detail').onclose = () => {state.detail++;};
$('export').onclick = () => {const url = URL.createObjectURL(new Blob([JSON.stringify(state.items, null, 2)], {type: 'application/json'})); const a = node('a'); a.href = url; a.download = `optipod-${state.view}.json`; a.click(); setTimeout(() => URL.revokeObjectURL(url), 1000);};
boot();
