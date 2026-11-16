'use strict';
(() => {
  const el = id => document.getElementById(id);
  const form = el('registrationform');
  let registrations = [], templates = [], selected = null, step = 0, health = null;
  const message = (text, error = false) => {el('onboardingstatus').textContent = text; el('onboardingstatus').className = error ? 'error' : '';};
  const post = (path, body, headers = {}) => request(path, {method: 'POST', headers: {'Content-Type': 'application/json', ...headers}, body: JSON.stringify(body)});
  const list = value => value.split(',').map(x => x.trim()).filter(Boolean);
  const guard = action => async event => {if (event) event.preventDefault(); try {await action(event);} catch (error) {message(error.message, true);}};
  function download(name, data, raw = false) {
    const url = URL.createObjectURL(new Blob([raw ? data : JSON.stringify(data, null, 2)], {type: raw ? 'text/plain' : 'application/json'}));
    const a = document.createElement('a'); a.href = url; a.download = name; a.click();
    setTimeout(() => URL.revokeObjectURL(url), 1000);
  }
  function action(label, callback) {const b = node('button', label); b.type = 'button'; b.onclick = guard(callback); return b;}
  function clean(r) {
    return {id:r.id, name:r.name, environment:r.environment || '', kubeUID:r.kubeUID, namespaces:r.namespaces || [], metricsSource:r.metricsSource, installMode:r.installMode, installMethod:r.installMethod, installation:{...r.installation}, revision:r.revision || 0, requireMTLS:!!r.requireMTLS};
  }
  function values() {
    const d = Object.fromEntries(new FormData(form));
    return {id:d.id, name:d.name, environment:d.environment, kubeUID:d.kubeUID, namespaces:list(d.namespaces), metricsSource:d.metricsSource, installMode:d.installMode, installMethod:d.installMethod,
      installation:Object.fromEntries(['namespace','release','controllerNamespace','controllerRelease','storageClass','prometheusURL'].map(k => [k,d[k]])), revision:selected?.revision || 0, requireMTLS:!!selected?.requireMTLS};
  }
  function fill(r) {
    form.reset(); const v = {...r, ...r.installation, namespaces:(r.namespaces || []).join(', ')};
    for (const [k,value] of Object.entries(v)) if (form.elements.namedItem(k)) form.elements.namedItem(k).value = value;
    form.elements.namedItem('id').readOnly = !!selected; form.elements.namedItem('kubeUID').readOnly = !!selected;
  }
  function showStep(next) {
    step = next; el('wizard').hidden = false; form.hidden = step > 2;
    for (const section of form.querySelectorAll('fieldset')) section.hidden = Number(section.dataset.step) !== step;
    [...el('wizardsteps').children].forEach((item,i) => {item.className = i === step ? 'current' : ''; if (i===step) item.setAttribute('aria-current','step'); else item.removeAttribute('aria-current');});
    el('wizardback').hidden = step === 0; el('wizardnext').hidden = step >= 2; el('saveregistration').hidden = step !== 2;
    el('connectstep').hidden = step !== 3; el('verifystep').hidden = step !== 4;
    el('wizardtitle').textContent = selected ? selected.name : 'Add cluster';
    if (selected) commands(selected);
  }
  function commands(r) {
    const file = `optipod-enrollment-${r.id}.json`, state = `"$HOME/.local/share/optipod/${r.id}"`;
    const target = '--kubeconfig /path/to/kubeconfig --context YOUR_CONTEXT';
    el('installcommands').textContent = `chmod 600 ${file}\noptipod-console plan --enrollment-file ${file} --plan-file optipod-plan-${r.id}.json\n# Review the plan, including optional private-network Secret references.\noptipod-console preflight --plan-file optipod-plan-${r.id}.json ${target}\noptipod-console onboard --plan-file optipod-plan-${r.id}.json --enrollment-file ${file} --state-dir ${state} ${target}${r.installMethod === 'gitops' ? ' --output-dir ./optipod-gitops-' + r.id : ''}\n# Resume by repeating onboard with the same plan and state directory.`;
    el('lifecyclecommands').textContent = `# Reconnect with a fresh enrollment package:\noptipod-console onboard --reenroll --enrollment-file ${file} --state-dir ${state} ${target}${r.installMethod === 'gitops' ? ' --output-dir ./optipod-gitops-' + r.id : ''}\n# Remove only a dedicated Helm agent release:\noptipod-console uninstall --plan-file optipod-plan-${r.id}.json --admin-token-file /private/admin-token ${target}`;
    el('wizardissue').disabled = r.disconnected;
  }
  async function saveStage(stage) {
    if (!selected) return;
    const r = clean(selected); r.installation.stage = stage;
    await request('/api/admin/clusters', {method:'PUT',headers:{'Content-Type':'application/json'},body:JSON.stringify(r)});
    await refreshAdministration();
  }
  async function issue(r) {
    const credential = await post('/api/admin/enrollments', {clusterID:r.id});
    download(`optipod-enrollment-${r.id}.json`, {...credential, serverURL:credential.serverURL || location.origin, kubeUID:r.kubeUID});
    message('Enrollment file downloaded. Protect it with chmod 600, use it within 10 minutes and keep it out of Git.');
    await refreshAdministration();
  }
  function healthNode(c) {
    const box = node('div',undefined,'onboardingrow');
    if (!c) {box.append(node('p','Awaiting this agent’s first complete inventory. Run the installer and refresh status.')); return box;}
    box.append(node('strong',c.id),node('small',`Inventory: ${c.inventoryReceived ? date(c.inventoryReceived) : 'not received'} · Heartbeat: ${c.heartbeatReceived ? date(c.heartbeatReceived) : 'not received'}`),node('small',`Agent ${c.agent.version || 'unknown'} · Kubernetes ${c.agent.kubernetesVersion || 'unknown'} · Queue ${c.agent.queueBytes || 0} / ${c.agent.queueLimit || 0} bytes`));
    for (const warning of c.warnings || []) box.append(node('p',warning,'notebox'));
    if (!c.warnings?.length) box.append(node('p','Telemetry is healthy. Review inventory before applying a Recommend policy.'));
    return box;
  }
  function renderHealth() {
    el('fleethealth').replaceChildren(node('p',`Database: ${health.database} · Console ${health.version}`));
    for (const c of health.clusters) el('fleethealth').append(healthNode(c));
    el('fleethealth').append(node('h3','Recovery evidence'));
    if (!health.recovery.length) el('fleethealth').append(node('p','No verified restore recorded. Configure backups and run the documented recovery test.'));
    for (const check of health.recovery) el('fleethealth').append(node('p',`${date(check.verifiedAt)} · ${check.kind} · schema ${check.schemaVersion}. This records a tested logical restore; it does not certify PITR or your recovery-time target.`));
    if (selected) el('wizardhealth').replaceChildren(healthNode(health.clusters.find(c => c.id === selected.id)));
  }
  async function refreshAdministration() {
    const [regs,credentials,events,report,notes,users,groups,presets] = await Promise.all(['/api/admin/clusters','/api/admin/credentials','/api/admin/audit','/api/admin/health','/api/admin/notifications','/api/admin/users','/api/admin/groups','/api/admin/templates'].map(path => request(path)));
    registrations = regs; health = report; templates = presets;
    if (selected) selected = registrations.find(r => r.id === selected.id) || null;
    for (const id of ['registrations','credentials','adminaudit','notifications','grants','templatechoice','templatecluster']) el(id).replaceChildren();
    const blank = node('option','Start from defaults'); blank.value = ''; el('templatechoice').append(blank);
    for (const t of templates) {const option = node('option',t.name); option.value=t.name; el('templatechoice').append(option);}
    for (const r of registrations) {
      const row=node('div',undefined,'onboardingrow');
      row.append(node('strong',r.name),node('small',`${r.id} · ${r.environment || 'No environment'} · ${r.disconnected ? 'Disconnected' : r.receivedAt ? 'Last received ' + date(r.receivedAt) : 'Awaiting first inventory'} · ${r.requireMTLS ? 'mTLS' : 'Bearer over HTTPS'}`));
      row.append(action('Resume setup',() => {selected=r;fill(r);showStep(r.installation?.stage === 'verify' ? 4 : 3);renderHealth();el('wizard').scrollIntoView({block:'start'});}));
      row.append(action(r.disconnected ? 'Reconnect' : 'Disconnect',async () => {
        if (!r.disconnected && !window.confirm(`Disconnect ${r.name} and revoke all its telemetry credentials? The optimizer and stored history remain.`)) return;
        await post('/api/admin/connection',{id:r.id,connected:r.disconnected}); await refreshAdministration(); message(r.disconnected ? 'Reconnected. Download a fresh enrollment package and use --reenroll.' : 'Telemetry disconnected; credentials revoked.');
      }));
      el('registrations').append(row);
      const option=node('option',r.name);option.value=r.id;el('templatecluster').append(option);
    }
    if (!registrations.length) el('registrations').append(node('p','No clusters registered yet. Add your first cluster to see its workloads.'));
    for (const c of credentials) {
      const row=node('div',undefined,'onboardingrow');const status=c.revokedAt ? 'Revoked' : c.usedAt ? 'Used' : new Date(c.expiresAt)<=new Date() ? 'Expired' : 'Active';
      row.append(node('strong',`${c.clusterID} · ${c.kind} · ${status}`),node('small',`Expires ${date(c.expiresAt)} · ${c.id}`));
      if (status==='Active') row.append(action('Revoke',async () => {if (!window.confirm(`Revoke this ${c.kind} credential for ${c.clusterID}?`)) return;await post('/api/admin/revoke',{id:c.id});await refreshAdministration();message('Credential revoked; history retained.');}));
      el('credentials').append(row);
    }
    for (const e of events) el('adminaudit').append(node('p',`${date(e.createdAt)} · ${e.actor} · ${e.action} · ${e.target}`));
    for (const n of notes) {const row=node('div',undefined,'onboardingrow');row.append(node('strong',`${n.clusterID} · ${n.kind}`),node('p',n.message),node('small',date(n.createdAt)));if (!n.acknowledgedAt) row.append(action('Acknowledge',async () => {await post('/api/admin/notifications',{id:n.id});await refreshAdministration();}));el('notifications').append(row);}
    if (!notes.length) el('notifications').append(node('p','No onboarding notifications.'));
    for (const [kind,items] of [['users',users],['groups',groups]]) for (const g of items) {
      const row=node('div',undefined,'onboardingrow');row.append(node('strong',`${g.subject} · ${g.role} · ${g.disabled ? 'Disabled' : 'Active'}`),node('small',`${kind} · Clusters: ${g.role==='admin' ? 'all' : g.clusters.join(', ') || 'none'} · Namespaces: ${g.namespaces.join(', ') || 'all within granted clusters'}`));
      row.append(action('Edit',() => {const f=el('grantform');for (const [k,v] of Object.entries({...g,kind,clusters:g.clusters.join(', '),namespaces:g.namespaces.join(', '),issueToken:false})) {const input=f.elements.namedItem(k);if(input){if(input.type==='checkbox')input.checked=!!v;else input.value=v;}}f.scrollIntoView();}));el('grants').append(row);
    }
    renderHealth();if(selected)commands(selected);
  }
  window.closeOnboarding = () => {for(const id of ['onboardingdialog','bootstrapdialog'])if(el(id).open)el(id).close();el('bootstrapform').reset();form.reset();selected=null;el('wizard').hidden=true;};
  el('openbootstrap').onclick=()=>el('bootstrapdialog').showModal();
  el('closebootstrap').onclick=()=>{el('bootstrapdialog').close();el('bootstrapform').reset();};
  el('closeonboarding').onclick=()=>el('onboardingdialog').close();
  el('bootstrapform').onsubmit=async event=>{event.preventDefault();el('bootstraperror').textContent='';const d=Object.fromEntries(new FormData(event.target));try{await post('/auth/bootstrap',{subject:d.subject,token:d.token},{Authorization:'Bearer '+d.bootstrap});event.target.reset();el('bootstrapdialog').close();await boot();}catch(error){el('bootstraperror').textContent=error.message;}};
  el('manageclusters').onclick=guard(async()=>{el('onboardingdialog').showModal();message('Loading onboarding…');await refreshAdministration();message('Saved cluster settings and setup progress are available here.');});
  el('refreshadmin').onclick=guard(refreshAdministration);
  el('addcluster').onclick=()=>{selected=null;fill({});showStep(0);};
  el('closewizard').onclick=()=>{el('wizard').hidden=true;};
  el('wizardnext').onclick=()=>{for(const input of form.querySelector(`fieldset[data-step="${step}"]`).querySelectorAll('input,select'))if(!input.reportValidity())return;showStep(step+1);};
  el('wizardback').onclick=()=>showStep(Math.max(0,step-1));
  el('editregistration').onclick=()=>{fill(selected);showStep(0);};
  form.onsubmit=guard(async()=>{const r=values();r.installation.stage='preflight';if(selected)await request('/api/admin/clusters',{method:'PUT',headers:{'Content-Type':'application/json'},body:JSON.stringify(r)});else await post('/api/admin/clusters',r);selected=r;await refreshAdministration();showStep(3);message('Registration saved. Download enrollment when you are ready to install.');});
  el('wizardissue').onclick=guard(async()=>{await saveStage('install');await issue(selected);});
  el('verifycluster').onclick=guard(async()=>{await saveStage('verify');showStep(4);renderHealth();el('policyform').elements.namedItem('namespace').value=selected.namespaces?.[0] || '';});
  el('backtoconnect').onclick=()=>showStep(3);
  el('policyform').onsubmit=guard(async event=>{if(!selected)return;const p=Object.fromEntries(new FormData(event.target));p.clusterID=selected.id;const result=await post('/api/admin/first-policy',p);download(`optipod-${p.name}.yaml`,result.yaml,true);message(result.guidance);});
  el('templatechoice').onchange=()=>{const t=templates.find(t=>t.name===el('templatechoice').value);if(t&&!selected){const current=values();fill({...t.registration,id:current.id,name:current.name,kubeUID:current.kubeUID});}};
  el('grantform').onsubmit=guard(async event=>{const d=Object.fromEntries(new FormData(event.target));const kind=d.kind;delete d.kind;d.clusters=list(d.clusters);d.namespaces=list(d.namespaces);d.disabled=!!d.disabled;d.issueToken=!!d.issueToken;if(d.role==='admin'&&(d.clusters.length||d.namespaces.length))throw new Error('Administrators manage all clusters. Clear the scope fields or choose Viewer.');if(kind==='groups'&&d.issueToken)throw new Error('SSO groups use verified sign-in claims; only users can receive tokens.');const result=await post('/api/admin/'+kind,d);if(result.token)download('optipod-user-token.txt',result.token,true);event.target.reset();await refreshAdministration();message(result.token ? 'Access saved. Protect the one-time downloaded token file with chmod 600.' : 'Access saved. Existing sessions use the updated grants.');});
  el('templateform').onsubmit=guard(async event=>{const d=Object.fromEntries(new FormData(event.target));const r=clean(registrations.find(r=>r.id===d.cluster));for(const k of ['id','name','kubeUID','revision','requireMTLS'])delete r[k];r.installation.stage='registered';await post('/api/admin/templates',{name:d.name,registration:r});await refreshAdministration();message('Environment template saved.');});
  el('bulkform').onsubmit=guard(async event=>{const data=JSON.parse(new FormData(event.target).get('registrations'));if(!Array.isArray(data)||!data.length||data.length>50)throw new Error('Provide an array of 1–50 cluster registrations.');await post('/api/admin/bulk',data);await refreshAdministration();message('Batch registered. Download a separate enrollment package for each cluster.');});
})();
